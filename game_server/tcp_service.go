package main

import (
	"container/list"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)
import (
	"github.com/go-redis/redis"
	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"roisoft.com/hdlj/common"
	pb "roisoft.com/hdlj/proto"
)

// Message infomation
type MsgInfo struct {
	mid_request uint32
	mid_reply   uint32
	head        []byte
	buf         []byte
}

// player info
type PlayerInfo struct {
	playerId uint64 // 玩家ID

	// 游戏相关属性
	playerState        uint8 // 状态
	matching_room_type uint8 // 房间类型
	room               *RoomInfo
	randSeed           uint32
	roleId             int32          // 选择的角色ID
	teamId             int32          // 队伍ID
	game_results       *pb.GameResult // 游戏结果

	// 连接相关属性
	sessionId    string    // 会话ID
	deadLine     time.Time // 会话有效期
	gameserverId uint32    // 游戏服务器ID
	agentId      uint32    // 代理服务器ID

	// 持久化数据
	dbData                 *pb.DbGameData
	last_save_time_redis   int64
	last_save_time_leveldb int64
	is_saved_redis         int32
	is_saved_leveldb       int32
}

// Session for a connection
type Session struct {
	recvBuffer     []byte
	recvPos        int
	recvTotal      int
	recvState      int
	recvMsgHead    pb.MessageHead
	chRecvingQueue chan IMessage
	recvFlag       bool

	sendBuffer     []byte
	sendPos        int
	sendTotal      int
	sendMsg        *MsgInfo
	chSendingQueue chan IMessage

	conn      *net.TCPConn
	connState uint32
	lastRecv  int64
	lastSend  int64

	playerInfo PlayerInfo
	mutex      *sync.RWMutex
}

// An uninteresting service.
type tcp_service struct {
	wg      *sync.WaitGroup
	l       *net.TCPListener
	counter int32
}

type ranking_action struct {
	s         *Session
	param     *pb.Player
	action_id int32
}

type pushing_message struct {
	msg_id         int
	msg_type       uint
	text           string
	ts_from, ts_to int64
	pushed         bool
}

// 公共常量
const (
	Maxsize_WaitingChannel = 128 // 发送/接受chan大小

	ConnState_UnAuth          = 1 // 未认证
	ConnState_Auth            = 2 // 已认证
	ConnState_WaitingForClose = 3 // 准备断开
	ConnState_Dead            = 4 // 已断开

	PlayerState_InHall   = 1 // 在游戏大厅
	PlayerState_Matching = 2 // 匹配中
	PlayerState_InRoom   = 3 // 在房间里准备游戏中
	PlayerState_Prepared = 4 // 已经准备好（场景加载完成）
	PlayerState_Playing  = 5 // 游戏中
	PlayerState_GameOver = 6 // 游戏结束

	redis_player_format       = "hdlj.game.player.%d"
	redis_player_list_format  = "hdlj.game.playerlist.%s"
	redis_ranking_list_format = "hdlj.game.rankinglist.%s"

	pushing_message_update_flag = "table/.new"
	pushing_message_file        = "table/PushingMessage.xlsx"

	RecvState_Head = 0
	RecvState_Body = 1

	RankingAction_Add = 1
	RankingAction_Get = 2

	robot_playerid = 1000000000
)

var (
	playerManager  sync.Map // 玩家管理器
	pushingManager sync.Map // 推送消息管理器

	chUnauthIoQueue chan *Session
	chOffQueue      chan *Session
	chSavingQueue   chan *Session

	chRankingAction chan *ranking_action
	rankingList     pb.RankingList

	redisWrite   *redis.Client
	redisRead    *redis.Client
	redisRanking *redis.Client

	dbCluster     common.DbCluster
	masterCluster common.MasterCluster

	total_player int32

	bStopServer = true
)

func NewTcpService() *tcp_service {
	return &tcp_service{
		wg: &sync.WaitGroup{},
		l:  nil,
	}
}

func (service *tcp_service) taskRoutine() {
	service.wg.Add(1)
	atomic.AddInt32(&service.counter, 1)
	defer service.wg.Done()
	defer atomic.AddInt32(&service.counter, -1)
	defer fmt.Println("routine exit: taskRoutine")

	ticker := time.NewTicker(time.Duration(cfg_task_interval))
	defer ticker.Stop()

	now := time.Now().UnixNano()
	ts_sync_player_amount := now
	ts_check_pushing_message := now

	notify_file := common.GetAbsFilePath(pushing_message_update_flag)
	msg_file := common.GetAbsFilePath(pushing_message_file)

	loadPushingTable(msg_file)
	os.Remove(notify_file)

	for range ticker.C {
		//fmt.Println("taskRoutine: loop")

		now = time.Now().UnixNano()

		if bStopServer {
			return
		}

		// sync player amount to master
		if now-ts_sync_player_amount >= int64(time.Second)*5 {
			ts_sync_player_amount = now

			for i := uint32(0); i < masterCluster.NodeTotal; i++ {
				master := masterCluster.GetNodeByIndex(i)
				if master == nil {
					continue
				}

				func() {
					ctx := context.Background()
					if cfg_grpc_client_timeout > 0 {
						var cancel context.CancelFunc
						ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
						defer cancel()
					}
					load_info := &pb.LoadInfo{NodeId: cfg_node_id, Load: total_player}
					master.SyncLoadInfo(ctx, load_info)
				}()
			}
		}

		// check pushing message file
		is_reload := false
		if common.CheckFileIsExist(notify_file) {
			is_reload = true

			// reload pushing message
			loadPushingTable(msg_file)

			os.Remove(notify_file)
		}
		// check pushing message date
		if is_reload || now-ts_check_pushing_message >= int64(time.Second)*60 {
			ts_check_pushing_message = now

			go pushingManager.Range(func(k interface{}, v interface{}) bool {
				msg := v.(*pushing_message)
				if msg.pushed == false && now >= msg.ts_from && now <= msg.ts_to {
					fmt.Printf("broadcast pushing message to all player. [message_id = %d]\n", k)
					msg.pushed = true

					// broadcast to all online player
					msg_body := pb.PushingMessage{Text: msg.text, MsgType: uint32(msg.msg_type), TimeStamp: now}
					msg_buf, _ := proto.Marshal(&msg_body)
					broadcastAll(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Pushing_Message, reply_buf: msg_buf})
				}
				return true
			})
		}
	}
}

func (service *tcp_service) ioRoutineUnauth(ch chan *Session) {
	service.wg.Add(1)
	atomic.AddInt32(&service.counter, 1)
	defer service.wg.Done()
	defer atomic.AddInt32(&service.counter, -1)
	defer fmt.Println("routine exit: ioRoutineUnauth")

	max_body := int32(pb.Constant_Message_MaxBody_Len) / 2

	for s := range ch {
		//fmt.Println("ioRoutineUnauth: loop.")

		bRemoveSession := false

		if bStopServer || s == nil {
			return
		}

		// check conn
		switch s.connState {
		case ConnState_Dead:
			continue
		case ConnState_WaitingForClose:
			bRemoveSession = true
		case ConnState_UnAuth:
			if cfg_session_timeout_auth > 0 && time.Now().UnixNano()-s.lastRecv >= cfg_session_timeout_auth {
				glog.Infof("连接验证超时(%d ms) [addr = %s]\n", cfg_session_timeout_auth/1e6, s.conn.RemoteAddr().String())
				bRemoveSession = true
			}
		case ConnState_Auth:
			//fmt.Printf("move session to auth queue. [player_id = %d, addr = %s]\n", s.playerInfo.playerId, s.conn.RemoteAddr().String())
			//go service.ioRoutineAuth(s)
			s.conn.SetDeadline(time.Time{})
			go service.iRoutineAuth(s)
			go service.oRoutineAuth(s)
			continue
		}

		// read task
		if !bRemoveSession {

			// read data from socket
			if s.recvFlag {
				read_left_n := 0
				if s.recvState == RecvState_Head {
					read_left_n = int(pb.Constant_Message_Head_Len) - (s.recvTotal - s.recvPos)
				} else if s.recvState == RecvState_Body && s.recvMsgHead.BodyLen > 0 {
					read_left_n = int(s.recvMsgHead.BodyLen) - (s.recvTotal - s.recvPos)
				}

				s.conn.SetReadDeadline(time.Now().Add(time.Nanosecond * time.Duration(cfg_io_timeout)))
				n, err := s.conn.Read(s.recvBuffer[s.recvTotal : s.recvTotal+read_left_n])
				if err != nil {
					if opErr, ok := err.(*net.OpError); err == io.EOF || (ok && !opErr.Timeout()) {
						glog.Infof("读消息失败 [err = %s, addr = %s]\n", err.Error(), s.conn.RemoteAddr().String())
						bRemoveSession = true
					}
				}
				if n > 0 {
					s.recvFlag = false
					s.recvTotal = s.recvTotal + n
				}
			}
			if s.recvState == RecvState_Head {
				if s.recvTotal-s.recvPos >= int(pb.Constant_Message_Head_Len) {
					if err := proto.Unmarshal(s.recvBuffer[s.recvPos:s.recvPos+int(pb.Constant_Message_Head_Len)], &s.recvMsgHead); err != nil {
						glog.Infof("消息头解析失败 [err = %s, addr = %s]\n", err.Error(), s.conn.RemoteAddr().String())
						bRemoveSession = true
					} else {
						s.recvState = RecvState_Body
						s.recvPos, s.recvTotal = 0, 0
						s.recvFlag = true
						s.lastRecv = time.Now().UnixNano()
					}
				} else {
					s.recvFlag = true
				}
			}
			if s.recvState == RecvState_Body {
				if s.recvMsgHead.BodyLen > max_body {
					glog.Infof("包体长度过大 [body len(%d) > %d, addr = %s]\n", s.recvMsgHead.BodyLen, max_body, s.conn.RemoteAddr().String())
					bRemoveSession = true
				} else if s.recvTotal-s.recvPos >= int(s.recvMsgHead.BodyLen) {

					// process data
					if s.recvMsgHead.BodyLen < 0 {
						s.recvMsgHead.BodyLen = 0
					}

					glog.V(common.Log_Info_Level_3).Infof("收到消息 [msg_id = %d, msg_bodylen = %d, from = %s]\n", s.recvMsgHead.MessageId, s.recvMsgHead.BodyLen, s.conn.RemoteAddr().String())
					//fmt.Printf("收到消息 [msg_id = %d, msg_bodylen = %d, from = %s]\n", s.recvMsgHead.MessageId, s.recvMsgHead.BodyLen, s.conn.RemoteAddr().String())

					// check first msessage
					if s.connState == ConnState_UnAuth && s.recvMsgHead.MessageId != uint32(pb.CS_MessageId_C2S_Signin_GameServer) {
						glog.Infof("未验证的连接，第一个消息不是登录消息 [msg_id = %d, addr = %s]\n", s.recvMsgHead.MessageId, s.conn.RemoteAddr().String())
						bRemoveSession = true
					} else {
						// 预处理
						if ok, err := preprocessMessage(pb.CS_MessageId(s.recvMsgHead.MessageId), s.recvBuffer[s.recvPos:s.recvPos+int(s.recvMsgHead.BodyLen)], s); !ok {
							glog.Infof("消息预处理失败 [err = %s, msg_id = %d, msg_bodylen = %d, from = %s]\n", err, s.recvMsgHead.MessageId, s.recvMsgHead.BodyLen, s.conn.RemoteAddr().String())
							bRemoveSession = true
						} else {
							// 处理
							msg := IMessage(nil)
							select {
							case msg = <-s.chRecvingQueue:
							default:
							}
							if msg != nil {
								if ok, err := msg.Handle(s); !ok {
									glog.Infof("消息处理失败 [err = %s, from = %s]\n", err, s.conn.RemoteAddr().String())
									bRemoveSession = true
								}
							}
						}
					}

					s.recvState = RecvState_Head
					s.recvPos, s.recvTotal = 0, 0
					s.recvFlag = true
					s.lastRecv = time.Now().UnixNano()
				} else {
					s.recvFlag = true
				}
			}

		}

		// write task
		if !bRemoveSession {
			// add msg to sending buffer
			for {
				if s.sendMsg == nil {
					select {
					case msg := <-s.chSendingQueue:
						if msg != nil {
							s.sendMsg = marshalMessage(msg)
						}
					default:
					}
				}
				if s.sendMsg == nil || len(s.sendMsg.buf)+len(s.sendMsg.head) > len(s.sendBuffer)-s.sendTotal {
					break
				}

				copy(s.sendBuffer[s.sendTotal:], s.sendMsg.head)
				s.sendTotal = s.sendTotal + len(s.sendMsg.head)
				copy(s.sendBuffer[s.sendTotal:], s.sendMsg.buf)
				s.sendTotal = s.sendTotal + len(s.sendMsg.buf)
				s.sendMsg = nil
			}

			if s.sendPos < s.sendTotal {
				// write data to socket from buffer
				s.conn.SetWriteDeadline(time.Now().Add(time.Nanosecond * time.Duration(cfg_io_timeout)))
				n, err := s.conn.Write(s.sendBuffer[s.sendPos:s.sendTotal])
				if err != nil {
					if opErr, ok := err.(*net.OpError); ok && !opErr.Timeout() {
						glog.Infof("发送消息失败 [err = %s, addr = %s]\n", err, s.conn.RemoteAddr().String())
						bRemoveSession = true
					}
				}
				if n > 0 {
					s.sendPos = s.sendPos + n
					if s.sendPos >= s.sendTotal {
						s.sendPos, s.sendTotal = 0, 0
					}

					s.lastSend = time.Now().UnixNano()
				}
			}
		}

		// remove session
		if bRemoveSession || bStopServer {
			s.Free()
		} else {
			ch <- s
		}
		//time.Sleep(time.Microsecond)
	}
}

func (service *tcp_service) iRoutineAuth(s *Session) {
	service.wg.Add(1)
	atomic.AddInt32(&service.counter, 1)
	defer service.wg.Done()
	defer atomic.AddInt32(&service.counter, -1)
	//defer fmt.Printf("routine exit: iRoutineAuth. [player_id = %d, addr = %s]\n", s.playerInfo.playerId, s.conn.RemoteAddr().String())

	max_body := int32(pb.Constant_Message_MaxBody_Len) / 2

	if s.recvTotal != 0 || s.recvPos != 0 {
		fmt.Printf("s.recvTotal != 0 || s.recvPos != 0. [player_id = %d, recvTotal = %d, recvPos = %d, addr = %s]\n", s.playerInfo.playerId, s.recvTotal, s.recvPos, s.conn.RemoteAddr().String())
		return
	}

	//s.conn.SetReadBuffer(0)

	bRemoveSession := false
	for {
		b_time := time.Now().UnixNano()

		// read head
		if !bRemoveSession && s.recvState == RecvState_Head {
			if cfg_session_timeout_heartbeat > 0 {
				s.conn.SetReadDeadline(time.Now().Add(time.Nanosecond * time.Duration(cfg_session_timeout_heartbeat-(b_time-s.lastRecv))))
			}
			//fmt.Println("iRoutineAuth read head. loop = ", counter)
			n, err := s.conn.Read(s.recvBuffer[0:pb.Constant_Message_Head_Len])
			if err != nil {
				glog.Infof("读消息失败 [err = %s, addr = %s, player_id = %d]\n", err.Error(), s.conn.RemoteAddr().String(), s.playerInfo.playerId)
				bRemoveSession = true
			} else if n < int(pb.Constant_Message_Head_Len) {
				bRemoveSession = true
			} else {
				if err := proto.Unmarshal(s.recvBuffer[0:pb.Constant_Message_Head_Len], &s.recvMsgHead); err != nil {
					glog.Infof("消息头解析失败 [err = %s, addr = %s, player_id = %d]\n", err.Error(), s.conn.RemoteAddr().String(), s.playerInfo.playerId)
					bRemoveSession = true
				} else {
					s.recvPos, s.recvTotal = 0, 0
					s.recvState = RecvState_Body
					s.lastRecv = time.Now().UnixNano()
				}
			}
		}
		if !bRemoveSession && s.recvState == RecvState_Body {
			if s.recvMsgHead.BodyLen >= max_body {
				glog.Infof("包体长度过大 [body len(%d) > %d, addr = %s, player_id = %d]\n", s.recvMsgHead.BodyLen, max_body, s.conn.RemoteAddr().String(), s.playerInfo.playerId)
				bRemoveSession = true
			} else {

				// read body
				if s.recvMsgHead.BodyLen > 0 {
					if cfg_session_timeout_heartbeat > 0 {
						s.conn.SetReadDeadline(time.Now().Add(time.Nanosecond * time.Duration(cfg_session_timeout_heartbeat-(b_time-s.lastRecv))))
					}
					//fmt.Println("iRoutineAuth read body. loop = ", counter)
					n, err := s.conn.Read(s.recvBuffer[0:s.recvMsgHead.BodyLen])
					if err != nil {
						glog.Infof("读消息失败 [err = %s, addr = %s]\n", err.Error(), s.conn.RemoteAddr().String())
						bRemoveSession = true
					} else if n < int(s.recvMsgHead.BodyLen) {
						bRemoveSession = true
					}
				}

				// process
				if !bRemoveSession {
					if s.recvMsgHead.BodyLen < 0 {
						s.recvMsgHead.BodyLen = 0
					}

					glog.V(common.Log_Info_Level_3).Infof("收到消息 [player_id = %d, msg_id = %d, msg_bodylen = %d, addr = %s, player_id = %d]\n", s.playerInfo.playerId, s.recvMsgHead.MessageId, s.recvMsgHead.BodyLen, s.conn.RemoteAddr().String(), s.playerInfo.playerId)
					if s.recvMsgHead.BodyLen > 40 {
						//fmt.Printf("收到大消息 [player_id = %d, msg_id = %d, msg_bodylen = %d, addr = %s]\n", s.playerInfo.playerId, s.recvMsgHead.MessageId, s.recvMsgHead.BodyLen, s.conn.RemoteAddr().String())
					}

					//fmt.Printf("iRoutineAuth [loop = %d, action = parse a message]\n", counter)
					if ok, err := processMessage(pb.CS_MessageId(s.recvMsgHead.MessageId), s.recvBuffer[0:s.recvMsgHead.BodyLen], s); !ok {
						glog.Infof("消息处理失败 [err = %s, msg_id = %d, msg_bodylen = %d, addr = %s, player_id = %d]\n", err, s.recvMsgHead.MessageId, s.recvMsgHead.BodyLen, s.conn.RemoteAddr().String(), s.playerInfo.playerId)
						bRemoveSession = true
					}

					s.recvPos, s.recvTotal = 0, 0
					s.recvState = RecvState_Head
					s.lastRecv = time.Now().UnixNano()
				}
			}
		}

		// remove session
		if bRemoveSession || bStopServer ||
			s.connState == ConnState_WaitingForClose ||
			s.connState == ConnState_Dead {
			s.conn.CloseWrite()
			s.chSendingQueue <- nil
			return
		}
	}
}

func (service *tcp_service) oRoutineAuth(s *Session) {
	service.wg.Add(1)
	atomic.AddInt32(&service.counter, 1)
	defer service.wg.Done()
	defer atomic.AddInt32(&service.counter, -1)
	//defer fmt.Printf("routine exit: oRoutineAuth. [player_id = %d, addr = %s]\n", s.playerInfo.playerId, s.conn.RemoteAddr().String())

	//s.conn.SetWriteBuffer(0)

	bRemoveSession := false
	for {

		// add msg to sending buffer
		loop_count := 0
		for !bRemoveSession {
			if loop_count >= 1 {
				//break
			}

			if s.sendMsg == nil {
				select {
				case msg := <-s.chSendingQueue:
					if msg == nil {
						bRemoveSession = true
					} else {
						s.sendMsg = marshalMessage(msg)
						loop_count++
					}
				default:
				}
			}
			if s.sendMsg == nil || len(s.sendBuffer)/2-s.sendTotal < len(s.sendMsg.buf)+len(s.sendMsg.head) {
				break
			}

			glog.V(common.Log_Info_Level_3).Infof("发送消息. [player_id = %d, msg_id = %d, body_len = %d, addr = %s]\n", s.playerInfo.playerId, s.sendMsg.mid_reply, len(s.sendMsg.buf), s.conn.RemoteAddr().String())
			if s.sendMsg.mid_reply == uint32(pb.CS_MessageId_S2C_Sync_Frame) {
				if len(s.sendMsg.buf) > 100 {
					//fmt.Printf("发送游戏帧消息. [player_id = %d, msg_id = %d, body_len = %d, addr = %s]\n", s.playerInfo.playerId, s.sendMsg.mid_reply, len(s.sendMsg.buf), s.conn.RemoteAddr().String())
				}
			}

			copy(s.sendBuffer[s.sendTotal:], s.sendMsg.head)
			s.sendTotal = s.sendTotal + len(s.sendMsg.head)
			copy(s.sendBuffer[s.sendTotal:], s.sendMsg.buf)
			s.sendTotal = s.sendTotal + len(s.sendMsg.buf)
			s.sendMsg = nil
		}

		if !bRemoveSession && s.sendPos < s.sendTotal {
			s.conn.SetWriteDeadline(time.Now().Add(time.Second * 3))
			if n, err := s.conn.Write(s.sendBuffer[s.sendPos:s.sendTotal]); err != nil {
				glog.Infof("发送消息失败 [err = %s, addr = %s, player_id = %d]\n", err, s.conn.RemoteAddr().String(), s.playerInfo.playerId)
				//atomic.CompareAndSwapUint32(&s.connState, ConnState_Auth, ConnState_WaitingForClose)
				bRemoveSession = true
			} else if n != s.sendTotal {
				glog.Infof("发送消息失败，数据没有一次发送完成 [write_n = %d, data_n = %d, addr = %s, player_id = %d]\n", n, s.sendTotal, s.conn.RemoteAddr().String(), s.playerInfo.playerId)
				//atomic.CompareAndSwapUint32(&s.connState, ConnState_Auth, ConnState_WaitingForClose)
				bRemoveSession = true
			} else {
				glog.V(common.Log_Info_Level_3).Infof("发送成功. [size = %d, addr = %s]\n", n, s.conn.RemoteAddr().String())

				cur_time := time.Now().UnixNano()
				// 发送时间间隔打印
				if delta := cur_time - s.lastSend; delta > int64(time.Millisecond)*40 {
					fmt.Printf("距离上一次发送消息成功，时间间隔. [player_id = %d, delta = %d(ms), addr = %s]\n", s.playerInfo.playerId, delta/int64(time.Millisecond), s.conn.RemoteAddr().String())
				}

				s.sendPos, s.sendTotal = 0, 0
				s.lastSend = cur_time
			}
		}

		// remove session
		if bRemoveSession || bStopServer ||
			s.connState == ConnState_WaitingForClose ||
			s.connState == ConnState_Dead {
			s.conn.CloseRead()
			s.Free()
			return
		}

		// read next message
		if s.sendPos == s.sendTotal {
			msg := <-s.chSendingQueue
			if msg == nil {
				bRemoveSession = true
			} else {
				s.sendMsg = marshalMessage(msg)
			}
		}
	}
}

func (service *tcp_service) saveRoutine(ch chan *Session) {
	service.wg.Add(1)
	atomic.AddInt32(&service.counter, 1)
	defer service.wg.Done()
	defer atomic.AddInt32(&service.counter, -1)
	defer fmt.Println("routine exit: saveRoutine")

	for s := range ch {
		fmt.Println("saveRoutine: loop")

		// 关闭服务器时，需要保证所有修改过的数据都已存盘
		if bStopServer && len(ch) <= 0 {
			return
		}

		// 忽略机器人
		if s == nil || s.playerInfo.playerId > robot_playerid {
			continue
		}

		// check conn state
		if s.connState != ConnState_Dead && s.connState != ConnState_WaitingForClose {

			// leveldb
			if atomic.CompareAndSwapInt32(&s.playerInfo.is_saved_leveldb, 0, 1) && time.Now().UnixNano()-s.playerInfo.last_save_time_leveldb >= cfg_player_data_save_interval_leveldb {
				if s.leveldbSave() {
					s.playerInfo.last_save_time_leveldb = time.Now().UnixNano()
					atomic.StoreInt32(&s.playerInfo.is_saved_redis, 0)
				} else {
					atomic.StoreInt32(&s.playerInfo.is_saved_leveldb, 0)
				}
			}
			// redis
			if atomic.CompareAndSwapInt32(&s.playerInfo.is_saved_redis, 0, 1) && time.Now().UnixNano()-s.playerInfo.last_save_time_redis >= cfg_player_data_save_interval_redis {
				if s.redisSave() {
					s.playerInfo.last_save_time_redis = time.Now().UnixNano()
				} else {
					atomic.StoreInt32(&s.playerInfo.is_saved_redis, 0)
				}
			}
		}
	}
}

func (service *tcp_service) offRoutine(ch chan *Session) {
	service.wg.Add(1)
	atomic.AddInt32(&service.counter, 1)
	defer service.wg.Done()
	defer atomic.AddInt32(&service.counter, -1)
	defer fmt.Println("routine exit: offRoutine")

	for s := range ch {
		fmt.Println("offRoutine: loop")

		// 关闭服务器时，需要保证所有修改过的数据都已存盘
		if bStopServer && len(ch) <= 0 {
			return
		}

		// 忽略机器人
		if s == nil || s.playerInfo.playerId > robot_playerid {
			continue
		}

		// leveldb
		if atomic.CompareAndSwapInt32(&s.playerInfo.is_saved_leveldb, 0, 1) {
			if s.leveldbSave() {
				cur_t := time.Now().UnixNano()

				s.playerInfo.dbData.LastSaveTime = cur_t
				s.playerInfo.last_save_time_leveldb = cur_t
				atomic.StoreInt32(&s.playerInfo.is_saved_redis, 0)

				// set offline
				s.setOffline()
			} else {
				atomic.StoreInt32(&s.playerInfo.is_saved_leveldb, 0)
			}
		}
		// redis
		if atomic.CompareAndSwapInt32(&s.playerInfo.is_saved_redis, 0, 1) {
			if s.redisSave() {
				s.playerInfo.last_save_time_redis = time.Now().UnixNano()
			} else {
				atomic.StoreInt32(&s.playerInfo.is_saved_redis, 0)
			}
		}

		if s.playerInfo.is_saved_leveldb <= 0 || s.playerInfo.is_saved_redis <= 0 {
			ch <- s
		} else {
			atomic.AddInt32(&total_player, -1)
		}
	}
}

func (service *tcp_service) rankingRoutine(ch chan *ranking_action) {
	service.wg.Add(1)
	atomic.AddInt32(&service.counter, 1)
	defer service.wg.Done()
	defer atomic.AddInt32(&service.counter, -1)
	defer fmt.Println("routine exit: rankingRoutine")

	save_interval := 0

	// load ranking from redis
	ranking_key := fmt.Sprintf(redis_ranking_list_format, cfg_node_id)
	if buf, err := redisRanking.Get(ranking_key).Bytes(); err == nil {
		proto.Unmarshal(buf, &rankingList)
	}

	for action := range ch {
		fmt.Println("rankingRoutine: loop")

		if action == nil {
			break
		}

		switch action.action_id {
		case RankingAction_Add:
			need_save := false
			total := len(rankingList.Players)

			if action.param != nil && action.s != nil && action.s.playerInfo.playerId < robot_playerid {
				if total < 10 || (total >= 10 && action.param.Exp > rankingList.Players[9].Exp) {
					need_save = true

					pos_new, pos_old := -1, -1
					for i, v := range rankingList.Players {
						if pos_new == -1 && action.param.Exp >= v.Exp {
							pos_new = i
						}
						if action.param.PlayerId == v.PlayerId {
							pos_old = i
						}
					}

					fmt.Printf("add a player to ranking list.\n")

					if pos_old != -1 && pos_old < pos_new {
						pos_new--
					}
					if pos_old != -1 {
						if pos_old+1 < total {
							rankingList.Players = append(rankingList.Players[:pos_old], rankingList.Players[pos_old+1:]...)
						} else {
							rankingList.Players = append(rankingList.Players[:pos_old], []*pb.Player{}...)
						}
					}
					if pos_new == -1 {
						rankingList.Players = append(rankingList.Players, action.param)
					} else {
						rear := append([]*pb.Player{}, rankingList.Players[pos_new:]...)
						rankingList.Players = append(rankingList.Players[:pos_new], action.param)
						rankingList.Players = append(rankingList.Players, rear...)
					}
				}
			}

			if need_save {
				save_interval++

				if len(rankingList.Players) > 10 {
					rankingList.Players = rankingList.Players[0:10]
				}

				if save_interval >= 100 {
					// save ranking to redis
					redis_is_ok := true
					if err := redisRanking.Ping().Err(); err != nil {
						redis_is_ok = false
						// reconn to redis
						if new_c, _ := getRedisClient(); new_c != nil {
							redisRanking = new_c
							redis_is_ok = true
						}
					}
					if redis_is_ok {
						buf, _ := proto.Marshal(&rankingList)
						if err := redisRanking.Set(ranking_key, buf, -1).Err(); err == nil {
							save_interval = 0
						}
					}
				}
			}
		case RankingAction_Get:
			if action.s != nil && action.s.playerInfo.playerId > 0 {
				fmt.Printf("ranking list: %v\n", rankingList)
				buf, _ := proto.Marshal(&rankingList)
				action.s.PostMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Rangking_Reply, reply_buf: buf})
			}
		}
	}

	buf, _ := proto.Marshal(&rankingList)
	redisRanking.Set(ranking_key, buf, -1)
}

func (s *tcp_service) Serve(addr string) (bool, string) {

	if ok, err := loadGameTable(cfg_table_path); !ok {
		return false, err
	}

	// 把所有db节点排序
	if !dbCluster.Sort() {
		return false, "fail to sort db node."
	}

	// 连接所有db
	for i, v := range dbCluster.Nodes {
		if conn, err := grpc.Dial(v.Addr, grpc.WithInsecure()); err != nil {
			return false, err.Error()
		} else {
			dbCluster.Conns[i] = conn
			dbCluster.Clients[i] = pb.NewDbServiceClient(conn)
		}
	}

	// 连接所有master
	for i, v := range masterCluster.Nodes {
		if conn, err := grpc.Dial(v.Addr, grpc.WithInsecure()); err != nil {
			return false, err.Error()
		} else {
			c := pb.NewMasterServiceClient(conn)
			masterCluster.Conns[i] = conn
			masterCluster.Clients[i] = c
		}
	}

	// 连接Redis
	if c, err := getRedisClient(); c == nil {
		return false, fmt.Sprintf("fail to connect to redis. [error = %v]", err)
	} else {
		redisWrite = c
	}
	if c, err := getRedisClient(); c == nil {
		return false, fmt.Sprintf("fail to connect to redis. [error = %v]", err)
	} else {
		redisRead = c
	}
	if c, err := getRedisClient(); c == nil {
		return false, fmt.Sprintf("fail to connect to redis. [error = %v]", err)
	} else {
		redisRanking = c
	}
	// 从Redis读取缓存玩家列表，把未保存的数据保存到db
	list_key := fmt.Sprintf(redis_player_list_format, cfg_node_id)
	if vals, err := redisWrite.SMembers(list_key).Result(); err == nil {
		n := int64(0)
		for v := range vals {
			key := fmt.Sprintf(redis_player_format, v)

			if buf, err := redisWrite.Get(key).Bytes(); err == nil {
				var rd pb.RGameDataInfo
				if proto.Unmarshal(buf, &rd) == nil && !rd.IsDbSaved {
					// save data to db
					leveldbSave2(rd.Data)
					setOffline2(rd.Data)
				}
			}

			n++
		}
		redisWrite.SPopN(list_key, n)
	}

	tpc_addr, err := net.ResolveTCPAddr("tcp", addr)
	if nil != err {
		return false, err.Error()
	}
	listener, err := net.ListenTCP("tcp", tpc_addr)
	if nil != err {
		return false, err.Error()
	}
	s.l = listener

	go func() {
		s.wg.Add(1)
		atomic.AddInt32(&s.counter, 1)
		defer s.wg.Done()
		defer atomic.AddInt32(&s.counter, -1)
		defer fmt.Printf("routine exit: listener routine\n")

		for {
			//listener.SetDeadline(time.Now().Add(time.Second * time.Duration(cfg_timeout_read)))
			conn, err := listener.AcceptTCP()
			if nil != err {
				if !bStopServer {
					glog.Fatalf("tcp listener error. [err = %s]\n", err)
				} else {
					glog.Infof("routine exit: tcp listener. [addr = %s]\n", addr)
				}
				return
			}
			glog.Infof("client connected [%s]\n", conn.RemoteAddr().String())
			fmt.Printf("client connected [%s]\n", conn.RemoteAddr().String())

			//conn.SetKeepAlive(true)
			//conn.SetKeepAlivePeriod()
			//conn.SetNoDelay(true)
			conn.SetReadBuffer(int(pb.Constant_Message_MaxBody_Len+pb.Constant_Message_Head_Len) * 2)
			conn.SetWriteBuffer(int(pb.Constant_Message_MaxBody_Len+pb.Constant_Message_Head_Len) * 4)

			session := new(Session)
			session.recvFlag = true
			session.recvBuffer = make([]byte, (pb.Constant_Message_MaxBody_Len+pb.Constant_Message_Head_Len)*2)
			session.sendBuffer = make([]byte, (pb.Constant_Message_MaxBody_Len+pb.Constant_Message_Head_Len)*4)
			session.recvState = RecvState_Head
			session.chRecvingQueue = make(chan IMessage, Maxsize_WaitingChannel)
			session.chSendingQueue = make(chan IMessage, Maxsize_WaitingChannel)
			session.conn = conn
			session.connState = ConnState_UnAuth
			session.playerInfo.playerId = 0
			session.playerInfo.sessionId = ""
			session.playerInfo.playerState = PlayerState_InHall
			session.playerInfo.room = nil
			session.playerInfo.dbData = nil
			session.lastRecv = time.Now().UnixNano()

			chUnauthIoQueue <- session
		}
	}()

	// 启动其他服务
	s.initRoomTask()
	go s.taskRoutine()

	chOffQueue = make(chan *Session, cfg_io_pool_size)
	chSavingQueue = make(chan *Session, cfg_io_pool_size)
	go s.offRoutine(chOffQueue)
	go s.saveRoutine(chSavingQueue)

	chUnauthIoQueue = make(chan *Session, cfg_io_pool_size)
	go s.ioRoutineUnauth(chUnauthIoQueue)

	chRankingAction = make(chan *ranking_action, cfg_io_pool_size)
	go s.rankingRoutine(chRankingAction)

	bStopServer = false

	return true, ""
}

func (s *tcp_service) Stop() {
	bStopServer = true

	s.l.Close()
	chUnauthIoQueue <- nil
	s.closeAllPlayerIoTask()
	s.closeRoomTask()
	chOffQueue <- nil
	chSavingQueue <- nil
	chRankingAction <- nil

	//time.Sleep(time.Second * 20)
	go func() {
		for {

			fmt.Println("Stop waiting. counter =", s.counter)
			time.Sleep(time.Second * 3)
		}
	}()
	fmt.Println("Stop waiting.")
	s.wg.Wait()
}

func (s *tcp_service) closeAllPlayerIoTask() {
	i := 0
	playerManager.Range(func(k interface{}, v interface{}) bool {
		s := v.(*Session)
		if s != nil && s.conn != nil && s.connState != ConnState_Dead && s.connState != ConnState_WaitingForClose {
			s.connState = ConnState_WaitingForClose
			s.chSendingQueue <- nil
			if cfg_session_timeout_heartbeat <= 0 {
				s.conn.Close()
			}
			i++
		}
		return true
	})
	fmt.Printf("close all client IO task: %d\n", i)
}

func (s *Session) Free() {
	defer fmt.Printf("client Free. [player_id = %d, player_state = %d, addr = %s]\n", s.playerInfo.playerId, s.playerInfo.playerState, s.conn.RemoteAddr().String())

	atomic.StoreUint32(&s.connState, ConnState_Dead)
	s.conn.Close()
	s.chSendingQueue <- nil

	// 已经登录的用户
	if s.playerInfo.playerId > 0 {
		if s.playerInfo.playerId < robot_playerid {
			now := time.Now().UnixNano()
			online_acc := (now - s.playerInfo.dbData.Game.OnlineLastTime) / int64(time.Second)
			s.playerInfo.dbData.Game.OnlineLastTime = now
			s.playerInfo.dbData.Game.OnlineAccumulate = s.playerInfo.dbData.Game.OnlineAccumulate + online_acc
			s.playerInfo.dbData.Game.OnlineAccumulateCm = s.playerInfo.dbData.Game.OnlineAccumulateCm + online_acc
			//s.setSavingRequirement()
			atomic.StoreInt32(&s.playerInfo.is_saved_leveldb, 0)
			atomic.StoreInt32(&s.playerInfo.is_saved_redis, 0)
			chOffQueue <- s
		}

		if s.playerInfo.playerState >= PlayerState_InRoom && s.playerInfo.room != nil && s.playerInfo.room.state != RoomState_GameOver && s.playerInfo.room.state != RoomState_NonNormal_GameOver {
			s.playerInfo.room.ch <- &RoomAction{action: RoomAction_Player_Disconn, frame_command: nil, player: s.playerInfo.playerId}
		} else if s.playerInfo.playerState == PlayerState_Matching && s.playerInfo.matching_room_type > 0 {
			chPlayerMatching[s.playerInfo.matching_room_type] <- &MatchingAction{s: s, action: MatchingAction_REM}
		} else {
			s.playerInfo.playerState = PlayerState_InHall
			s.playerInfo.matching_room_type = 0
			s.playerInfo.room = nil
		}

		if s.playerInfo.playerState != PlayerState_Playing {
			playerManager.Delete(s.playerInfo.playerId)
		}
	}
}

func (s *Session) PostMessage(msg IMessage) bool {
	if s.connState == ConnState_Dead || s.connState == ConnState_WaitingForClose {
		return false
	}

	if len(s.chSendingQueue) >= Maxsize_WaitingChannel {
		glog.Infof("player sending queue is full. [player_id = %d, addr = %s]\n", s.playerInfo.playerId, s.conn.RemoteAddr().String())
		atomic.CompareAndSwapUint32(&s.connState, ConnState_Auth, ConnState_WaitingForClose)
		atomic.CompareAndSwapUint32(&s.connState, ConnState_UnAuth, ConnState_WaitingForClose)
		return false
	} else {
		s.chSendingQueue <- msg
	}
	return true
}

func BroadcastMessage(msg IMessage, players *list.List, sender uint64, include_me bool) {

	//if msg.GetMessageId(true) == uint32(pb.CS_MessageId_C2C_Broadcast_Room) {
	//	fmt.Println("Broadcast Room Message: ", msg.ReplyToBuffer())
	//}

	for e := players.Front(); e != nil; e = e.Next() {
		cur_s := e.Value.(*Session)

		//fmt.Printf("broadcast message. [msg_id = %d, player = %d]\n", msg_id, cur_s.playerInfo.playerId)
		if cur_s.connState == ConnState_Dead || cur_s.connState == ConnState_WaitingForClose || !include_me && cur_s.playerInfo.playerId == sender {
			continue
		}
		cur_s.PostMessage(msg)
	}
}

func preprocessMessage(msg_id pb.CS_MessageId, buf []byte, s *Session) (bool, string) {

	if msg_id == pb.CS_MessageId_C2S_HeartBeat {
		s.lastRecv = time.Now().UnixNano()
		//fmt.Println("heart beat")
		return true, ""
	} else if msgfactory, ok := gMsgFactory[pb.CS_MessageId(msg_id)]; ok {
		// parse the message
		msg := msgfactory()
		if ok, err := msg.RequestFromBuffer(buf); !ok {
			return false, err
		}

		// put the message into recving queue
		if len(s.chRecvingQueue) >= Maxsize_WaitingChannel {
			glog.Infof("player recving queue is full. [player_id = %d, addr = %s]\n", s.playerInfo.playerId, s.conn.RemoteAddr().String())
		} else {
			s.chRecvingQueue <- msg
			//msg.Handle(s)
		}

		return true, ""
	} else {
		//fmt.Println("room message")
		// 转发消息(房间)
		if s.playerInfo.playerState >= PlayerState_InRoom && s.playerInfo.room != nil {
			room := s.playerInfo.room
			includeSelf := true
			if msg_id == pb.CS_MessageId_C2C_Broadcast_Room_ExSelf {
				includeSelf = false
			}
			//fmt.Println("room message:", buf)
			tmp_buf := make([]byte, len(buf))
			copy(tmp_buf, buf)
			BroadcastMessage(&MsgCommon{reply_message_id: msg_id, reply_buf: tmp_buf}, &room.players, s.playerInfo.playerId, includeSelf)
			return true, ""
		}
	}

	return false, "preprocessMessage() invalid message."
}

func processMessage(msg_id pb.CS_MessageId, buf []byte, s *Session) (bool, string) {

	if msg_id == pb.CS_MessageId_C2S_HeartBeat {
		s.lastRecv = time.Now().UnixNano()
		//fmt.Println("heart beat")
		return true, ""
	} else if msgfactory, ok := gMsgFactory[pb.CS_MessageId(msg_id)]; ok {
		// parse the message
		msg := msgfactory()
		if ok, err := msg.RequestFromBuffer(buf); !ok {
			return false, err
		}

		// put the message into recving queue
		if len(s.chRecvingQueue) >= Maxsize_WaitingChannel {
			glog.Infof("player recving queue is full. [player_id = %d, addr = %s]\n", s.playerInfo.playerId, s.conn.RemoteAddr().String())
		} else {
			if ok, err := msg.Handle(s); !ok {
				glog.Infof("消息处理失败 [err = %s, from = %s]\n", err, s.conn.RemoteAddr().String())
				return false, err
			}
		}

		return true, ""
	} else {
		//fmt.Println("room message")
		// 转发消息(房间)
		if s.playerInfo.playerState >= PlayerState_InRoom && s.playerInfo.room != nil {
			room := s.playerInfo.room
			includeSelf := true
			if msg_id == pb.CS_MessageId_C2C_Broadcast_Room_ExSelf {
				includeSelf = false
			}
			//fmt.Println("room message:", buf)
			tmp_buf := make([]byte, len(buf))
			copy(tmp_buf, buf)
			BroadcastMessage(&MsgCommon{reply_message_id: msg_id, reply_buf: tmp_buf}, &room.players, s.playerInfo.playerId, includeSelf)
			return true, ""
		}
	}

	return false, "processMessage() invalid message."
}

func marshalMessage(msg IMessage) *MsgInfo {
	var head pb.MessageHead

	buf := msg.ReplyToBuffer()
	msg_id := msg.GetMessageId(true)

	if msg_id == 0 {
		return nil
	}

	head.MessageId = msg_id
	if len(buf) <= 0 {
		head.BodyLen = -1
	} else {
		head.BodyLen = int32(len(buf))
	}
	buf_head, _ := proto.Marshal(&head)

	if head.MessageId == uint32(pb.CS_MessageId_C2C_Broadcast_Room) {
		//fmt.Printf("serialize message. [msg_id = %d, body_len = %d]\n", head.MessageId, head.BodyLen)
		//fmt.Println(buf)
	}

	return &MsgInfo{mid_request: msg.GetMessageId(false), mid_reply: msg.GetMessageId(true), head: buf_head, buf: buf}
}

func (s *Session) redisSave() bool {

	// check redis connection
	if err := redisWrite.Ping().Err(); err != nil {
		redis_is_ok := false
		if new_c, _ := getRedisClient(); new_c != nil {
			redisWrite = new_c
			redis_is_ok = true
		}
		if !redis_is_ok {
			return false
		}
	}

	// save data
	is_saved := atomic.LoadInt32(&s.playerInfo.is_saved_leveldb) > 0
	key := fmt.Sprintf(redis_player_format, s.playerInfo.playerId)
	data := &pb.RGameDataInfo{Data: s.playerInfo.dbData, IsDbSaved: is_saved}
	buf, _ := proto.Marshal(data)
	if err := redisWrite.Set(key, buf, time.Hour*time.Duration(cfg_player_data_expiration_in_hour)).Err(); err == nil {
		// 操作Redis的玩家列表
		key := fmt.Sprintf(redis_player_list_format, cfg_node_id)
		if is_saved {
			redisWrite.SRem(key, s.playerInfo.playerId)
		} else {
			redisWrite.SAdd(key, s.playerInfo.playerId)
		}
		return true
	} else {
		glog.Errorf("save data to redis error. [playerid = %d, error = %v]\n", s.playerInfo.playerId, err)
	}

	return false
}

func (s *Session) leveldbSave() bool {
	return leveldbSave2(s.playerInfo.dbData)
}

func leveldbSave2(data *pb.DbGameData) bool {
	db := dbCluster.GetNodeByPlayerId(data.PlayerId)
	if db == nil {
		return false
	}

	// save data
	ctx := context.Background()
	if cfg_grpc_client_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
		defer cancel()
	}
	buf_key, _ := proto.Marshal(&pb.KeyPlayerId{Key: data.PlayerId})
	buf_value, _ := proto.Marshal(data)
	request := &pb.DbRequest{Type: uint32(pb.OpType_Game), Method: uint32(pb.OpMethod_PUT), Key: buf_key, Value: buf_value}

	if _, err := db.DbOperation(ctx, request); err == nil {
		return true
	} else {
		glog.Errorf("save data to leveldb error. [playerid = %d, error = %v]\n", data.PlayerId, err)
	}

	return false
}

func (s *Session) setOffline() bool {
	return setOffline2(s.playerInfo.dbData)
}

func setOffline2(data *pb.DbGameData) bool {
	ctx := context.Background()
	if cfg_grpc_client_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
		defer cancel()
	}
	request := &pb.SessionRequest{PlayerId: data.PlayerId}
	master := masterCluster.GetNodeByPlayerId(data.PlayerId)

	if _, err := master.Offline(ctx, request); err == nil {
		return true
	} else {
		glog.Errorf("set offline error. [playerid = %d, error = %v]\n", data.PlayerId, err)
	}

	return false
}

func (s *Session) setSavingRequirement() {
	atomic.StoreInt32(&s.playerInfo.is_saved_leveldb, 0)
	atomic.StoreInt32(&s.playerInfo.is_saved_redis, 0)

	chSavingQueue <- s
}

func getRedisClient() (*redis.Client, string) {
	c := redis.NewClient(&redis.Options{
		Addr:        cfg_server_addr_redis,
		Password:    cfg_server_pwd_redis,
		DB:          0, // use default DB
		MaxRetries:  3,
		DialTimeout: time.Second * 5,
	})
	if err := c.Ping().Err(); err != nil {
		return nil, err.Error()
	}
	return c, ""
}

func broadcastAll(msg IMessage) {
	playerManager.Range(func(k interface{}, v interface{}) bool {
		s := v.(*Session)
		if s != nil && s.conn != nil && s.connState != ConnState_Dead && s.connState != ConnState_WaitingForClose {
			s.PostMessage(msg)
		}
		return true
	})
}
