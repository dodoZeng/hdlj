package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/satori/go.uuid"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"roisoft.com/hdlj/common"
	pb "roisoft.com/hdlj/proto"
)

type RoomInfo struct {
	roomId      uint32
	roomType    uint8
	startTime   int64
	tmPerFrame  int64
	lastCommand int64
}

type PlayerInfo struct {
	playerId  uint64 // 玩家ID
	sessionId string // 会话ID
	room      *RoomInfo
	randSeed  uint32
}

type MsgInfo struct {
	mid  uint32
	head []byte
	buf  []byte
}

type Client struct {
	step uint // 阶段

	recvBuffer  []byte
	recvPos     int
	recvTotal   int
	recvState   int
	recvMsgHead pb.MessageHead
	recvFlag    bool

	sendBuffer []byte
	sendPos    int
	sendTotal  int
	sendCh     chan bool

	conn      *net.TCPConn
	connState uint8

	playerInfo  PlayerInfo
	server_time int64
	local_time  int64

	lastSend int64
	lastRecv int64

	ticker *time.Ticker
}

type RoleInfo struct {
	PlayerId      uint64
	SelectCharaNo int32
}

const (
	Step_ToAuth       = 0
	Step_ToSyncTime   = 1
	Step_ToMatch      = 2
	Step_ToSelectRole = 3
	Step_ToLoad       = 4
	Step_ToPlay       = 5

	Step_Sent = 99999999

	RecvState_Head = 0
	RecvState_Body = 1
)

var (
	ch             chan bool
	chClients      chan *Client
	counterClients int32

	cfg_client_total           int   = 1 * 1                             // 模拟多少个客户端
	cfg_command_num_per_second int64 = 3                                 // 模拟每秒发送多少个游戏指令
	cfg_heartbeat_interval     int64 = 5 * 1e9                           // 心跳发送间隔
	cfg_role_id                      = 3                                 // 模拟选择角色ID
	cfg_room_type                    = uint32(pb.RoomType_MapName_5v5_4) // 模拟房间类型ID
	cfg_drop_game_frame              = true                              // 是否丢掉游戏帧数据（排除接收不及时的可能）
	cfg_log_dir                      = "./log"

	cfg_test_game_server_addr = "193.112.132.240:6000"
	//cfg_test_game_server_addr = "192.168.0.114:6000"
)

func IntToBytes(n uint64) []byte {
	tmp := uint64(n)
	bytesBuffer := bytes.NewBuffer([]byte{})
	binary.Write(bytesBuffer, binary.LittleEndian, tmp)
	return bytesBuffer.Bytes()
}

func test_account_server() {
	// Set up a connection to the server.
	conn, err := grpc.Dial("localhost:50001", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewAccountServiceClient(conn)

	account := fmt.Sprintf("%d", time.Now().UnixNano())
	pwd := "pwd"

	// signup
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if _, err := c.Signup(ctx, &pb.SignupRequest{AccountType: int32(pb.AccountType_Guest), Account: account, Password: pwd, AttachCode: string("da1272f4d15721c81c237427aba984ae")}); err != nil {
		if grpc.Code(err) == codes.Unknown {
			log.Println("signup internal error.")
		} else {
			log.Printf("signup grpc error: %v\n", err)
		}
	} else {
		log.Printf("signup ok.\n")
	}

	// signin
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if r, err := c.Signin(ctx, &pb.SigninRequest{AccountType: int32(pb.AccountType_Guest), Account: account, Password: pwd}); err != nil {
		if grpc.Code(err) == codes.Unknown {
			log.Println("signup internal error.")
		} else {
			log.Printf("signup grpc error: %v\n", err)
		}
	} else {
		log.Printf("signin ok. [return = %v]\n", r)
		test_master_server(r.SessionId, r.PlayerId)
	}

	ch <- true
}

func test_account_server2() {
	// Set up a connection to the server.
	conn, err := grpc.Dial("localhost:50001", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewAccountServiceClient(conn)

	account := fmt.Sprintf("%d", time.Now().UnixNano())
	pwd := "pwd"

	// signin
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if r, err := c.Signin(ctx, &pb.SigninRequest{AccountType: int32(pb.AccountType_Guest), Account: account, Password: pwd}); err != nil {
		if grpc.Code(err) == codes.Unknown {
			log.Println("signup internal error.")
		} else {
			log.Printf("signup grpc error: %v\n", err)
		}
	} else {
		log.Printf("signin ok. [return = %v]\n", r)
		test_master_server(r.SessionId, r.PlayerId)
	}

	ch <- true
}

func test_db_server() {
	conn, err := grpc.Dial("localhost:40000", grpc.WithInsecure())
	//conn, err := grpc.Dial("192.168.0.100:40000", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewDbServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	key_buf, _ := proto.Marshal(&pb.KeyString{Key: string("dodo")})
	dbrequest := &pb.DbRequest{Type: uint32(pb.OpType_Account), Method: uint32(pb.OpMethod_CHK), Key: key_buf}
	if _, err := c.DbOperation(ctx, dbrequest); err != nil {
		if grpc.Code(err) == codes.Unknown {
			log.Printf("db_server internal error. [err = %v]\n", err)
		} else {
			log.Printf("db_server grpc error. [err = %v]\n", err)
		}
	} else {
		log.Printf("account is exist.\n")
	}

	val_buf, _ := proto.Marshal(&pb.KeyPlayerId{Key: uint64(888)})
	dbrequest = &pb.DbRequest{Type: uint32(pb.OpType_Account_Index), Method: uint32(pb.OpMethod_PUT), Key: key_buf, Value: val_buf}
	if _, err := c.DbOperation(ctx, dbrequest); err != nil {
		if grpc.Code(err) == codes.Unknown {
			log.Printf("db_server internal error. [err = %v]\n", err)
		} else {
			log.Printf("db_server grpc error. [err = %v]\n", err)
		}
	} else {
		log.Printf("add index ok.\n")
	}

	data_buf, _ := proto.Marshal(&pb.DbAccountData{PlayerId: uint64(888), Password: string("pwd"), SignupTime: time.Now().UnixNano()})
	dbrequest = &pb.DbRequest{Type: uint32(pb.OpType_Account), Method: uint32(pb.OpMethod_PUT), Key: val_buf, Value: data_buf}
	if _, err := c.DbOperation(ctx, dbrequest); err != nil {
		if grpc.Code(err) == codes.Unknown {
			log.Printf("db_server internal error. [err = %v]\n", err)
		} else {
			log.Printf("db_server grpc error. [err = %v]\n", err)
		}
	} else {
		log.Printf("add account data ok.\n")
	}

	ch <- true
}

func test_master_server(sid string, playerid uint64) {
	// Set up a connection to the server.
	conn, err := grpc.Dial("localhost:50000", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewMasterServiceClient(conn)

	// check session
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if r, err := c.CheckSession(ctx, &pb.SessionRequest{SessionId: sid, PlayerId: playerid}); err != nil {
		if grpc.Code(err) == codes.Unknown {
			log.Println("check session internal error.")
		} else {
			log.Printf("check session grpc error: %v\n", err)
		}
	} else {
		log.Printf("check session ok. [return = %v]\n", r)
	}
}

func messageMake(c *Client) {

	switch c.step {
	case Step_ToAuth:
		uid, _ := uuid.NewV4()
		c.playerInfo.playerId = uint64(binary.LittleEndian.Uint64(uid[:]))

		var head pb.MessageHead
		var body pb.SigninRequest

		body.PlayerId = c.playerInfo.playerId
		body.SessionId = fmt.Sprintf("%d", c.playerInfo.playerId)
		buf, _ := proto.Marshal(&body)

		head.MessageId = uint32(pb.CS_MessageId_C2S_Signin_GameServer)
		head.BodyLen = int32(len(buf))
		buf_h, _ := proto.Marshal(&head)

		// add msg to sending buffer
		if len(c.sendBuffer)-c.sendTotal >= len(buf_h)+len(buf) {
			copy(c.sendBuffer[c.sendTotal:], buf_h)
			c.sendTotal = c.sendTotal + len(buf_h)
			copy(c.sendBuffer[c.sendTotal:], buf)
			c.sendTotal = c.sendTotal + len(buf)
			fmt.Println("make a Signin request.")

			c.step = Step_Sent
			c.sendCh <- true
		}

	case Step_ToSyncTime:
		var head pb.MessageHead
		var body pb.PingRequest

		body.Client = &pb.MessageSpan{}
		body.Client.Send = time.Now().UnixNano()
		buf, _ := proto.Marshal(&body)

		head.MessageId = uint32(pb.CS_MessageId_C2S_Synchronize_Time_Request)
		head.BodyLen = int32(len(buf))
		buf_h, _ := proto.Marshal(&head)

		// add msg to sending buffer
		if len(c.sendBuffer)-c.sendTotal >= len(buf_h)+len(buf) {
			copy(c.sendBuffer[c.sendTotal:], buf_h)
			c.sendTotal = c.sendTotal + len(buf_h)
			copy(c.sendBuffer[c.sendTotal:], buf)
			c.sendTotal = c.sendTotal + len(buf)
			fmt.Println("make a SyncTime request.")

			c.step = Step_Sent
			c.sendCh <- true
		}

	case Step_ToMatch:
		var head pb.MessageHead
		var body pb.PlayerMatchingRequest

		body.RoomType = cfg_room_type
		buf, _ := proto.Marshal(&body)

		head.MessageId = uint32(pb.CS_MessageId_C2S_Start_PlayerMatching)
		head.BodyLen = int32(len(buf))
		buf_h, _ := proto.Marshal(&head)

		if len(c.sendBuffer)-c.sendTotal >= len(buf_h)+len(buf) {
			copy(c.sendBuffer[c.sendTotal:], buf_h)
			c.sendTotal = c.sendTotal + len(buf_h)
			copy(c.sendBuffer[c.sendTotal:], buf)
			c.sendTotal = c.sendTotal + len(buf)
			fmt.Println("make a Match request.")

			c.step = Step_Sent
			c.sendCh <- true
		}

	case Step_ToSelectRole:
		var head pb.MessageHead

		var equips [3]uint32
		body := pb.SyncRole{PlayerId: c.playerInfo.playerId, HeroId: uint32(cfg_role_id), EquipIds: equips[0:]}
		buf, _ := proto.Marshal(&body)

		//fmt.Println(len(buf))
		//fmt.Println(buf)

		head.MessageId = uint32(pb.CS_MessageId_C2C_Broadcast_Room)
		head.BodyLen = int32(len(buf))
		buf_h, _ := proto.Marshal(&head)

		if len(c.sendBuffer)-c.sendTotal >= len(buf_h)+len(buf) {
			copy(c.sendBuffer[c.sendTotal:], buf_h)
			c.sendTotal = c.sendTotal + len(buf_h)
			copy(c.sendBuffer[c.sendTotal:], buf)
			c.sendTotal = c.sendTotal + len(buf)
			fmt.Println("sync role. player_id = ", c.playerInfo.playerId)

			c.step = Step_Sent
			c.sendCh <- true
		}

	case Step_ToLoad:
		var head pb.MessageHead
		var body pb.PlayerState

		body.PlayerId = c.playerInfo.playerId
		body.Percent = 100
		buf, _ := proto.Marshal(&body)

		head.MessageId = uint32(pb.CS_MessageId_C2S_Sync_LoadingProgress)
		head.BodyLen = int32(len(buf))
		buf_h, _ := proto.Marshal(&head)

		//time.Sleep(time.Second * 10)

		if len(c.sendBuffer)-c.sendTotal >= len(buf_h)+len(buf) {
			copy(c.sendBuffer[c.sendTotal:], buf_h)
			c.sendTotal = c.sendTotal + len(buf_h)
			copy(c.sendBuffer[c.sendTotal:], buf)
			c.sendTotal = c.sendTotal + len(buf)
			fmt.Println("sync Loading progress.")

			c.step = Step_Sent
			c.sendCh <- true
		}

	case Step_ToPlay:
		if c.ticker == nil {
			//fmt.Println("set a game command ticker")

			if cfg_command_num_per_second > 0 {
				c.ticker = time.NewTicker(time.Duration(int64(time.Second) / cfg_command_num_per_second))
				go func(c *Client) {
					for range c.ticker.C {
						var head pb.MessageHead
						var body pb.FrameCommand
						var data [10]byte

						cur_server_time := c.server_time + (time.Now().UnixNano() - c.local_time)
						delta_time := cur_server_time - c.playerInfo.room.startTime
						body.FrameId = uint32(delta_time / c.playerInfo.room.tmPerFrame)
						body.Commands = &pb.GameCommand{Data: data[0:]}
						body.TimeSend = cur_server_time
						buf, _ := proto.Marshal(&body)
						//fmt.Println("frame TimeSend:", body.TimeSend)

						head.MessageId = uint32(pb.CS_MessageId_C2S_Sync_PlayerOperation)
						head.BodyLen = int32(len(buf))
						buf_h, _ := proto.Marshal(&head)

						//fmt.Println("command package len = ", len(buf_h)+len(buf))

						if len(c.sendBuffer)-c.sendTotal >= len(buf_h)+len(buf) {

							//fmt.Println("send game command.")
							copy(c.sendBuffer[c.sendTotal:], buf_h)
							c.sendTotal = c.sendTotal + len(buf_h)
							copy(c.sendBuffer[c.sendTotal:], buf)
							c.sendTotal = c.sendTotal + len(buf)

							c.playerInfo.room.lastCommand = time.Now().UnixNano()
							c.sendCh <- true
						}
					}
				}(c)
			} else {
				c.ticker = time.NewTicker(time.Duration(cfg_heartbeat_interval))
				go func(c *Client) {
					for range c.ticker.C {
						var head pb.MessageHead

						head.MessageId = uint32(pb.CS_MessageId_C2S_HeartBeat)
						head.BodyLen = -1
						buf_h, _ := proto.Marshal(&head)

						if len(c.sendBuffer)-c.sendTotal >= len(buf_h) {

							//fmt.Println("send a heartbeat.")
							copy(c.sendBuffer[c.sendTotal:], buf_h)
							c.sendTotal = c.sendTotal + len(buf_h)

							c.sendCh <- true
						}
					}
				}(c)
			}

		}
	}
}

func messageHandle(msg_id uint32, buf []byte, c *Client) (bool, string) {

	switch msg_id {
	case uint32(pb.CS_MessageId_S2C_Signin_GameServer_Reply):
		var reply pb.ErrorInfo
		proto.Unmarshal(buf, &reply)

		fmt.Println("recv signin reply.")
		if reply.Code == int32(pb.ErrorCode_CS_OK) {
			fmt.Println("signin ok")

			c.step = Step_ToSyncTime
			messageMake(c)

		} else {
			return false, reply.Context
		}
	case uint32(pb.CS_MessageId_S2C_Synchronize_Time_Reply):
		var reply pb.PingReply
		proto.Unmarshal(buf, &reply)

		reply.ClientSpan.Recv = time.Now().UnixNano()

		c.server_time = reply.ServerCurTime //+ (reply.ClientSpan.Recv-reply.ClientSpan.Send-(reply.ServerSpan.Send-reply.ServerSpan.Recv))/2
		c.local_time = time.Now().UnixNano()
		c.step = Step_ToMatch
		messageMake(c)

	case uint32(pb.CS_MessageId_S2C_Start_PlayerMatching_Reply):
		var reply pb.ErrorInfo
		proto.Unmarshal(buf, &reply)

		if reply.Code == int32(pb.ErrorCode_CS_OK) {
			fmt.Println("matching ok.")
		} else {
			return false, reply.Context
		}

	case uint32(pb.CS_MessageId_S2C_Create_Room):
		var reply pb.Room
		proto.Unmarshal(buf, &reply)

		c.playerInfo.room = &RoomInfo{roomId: uint32(reply.RoomId)}
		c.playerInfo.room.tmPerFrame = int64(time.Second) / int64(reply.GameFrameNumPerSecond)
		fmt.Println("room created.")

		c.step = Step_ToSelectRole
		messageMake(c)

	case uint32(pb.CS_MessageId_S2C_Destroy_Room):
	case uint32(pb.CS_MessageId_C2C_Broadcast_Room):
		var reply pb.SyncRole
		if err := proto.Unmarshal(buf, &reply); err == nil {

			fmt.Println("recv role info.")
			c.step = Step_ToLoad
			messageMake(c)
		}

	case uint32(pb.CS_MessageId_S2C_Sync_LoadingProgress):

	case uint32(pb.CS_MessageId_S2C_Start_Game):
		var reply pb.GameInfo
		proto.Unmarshal(buf, &reply)

		c.playerInfo.room.startTime = int64(reply.GameStartTime)
		fmt.Printf("game start. [player = %d, addr = %s]\n", c.playerInfo.playerId, c.conn.LocalAddr().String())
		c.step = Step_ToPlay
		messageMake(c)

	case uint32(pb.CS_MessageId_S2C_Sync_Frame):

	case uint32(pb.CS_MessageId_S2C_Sync_GameOver):
	}

	return true, ""
}

func test_game_server(client_total int) {

	wg := sync.WaitGroup{}
	chClients = make(chan *Client, client_total)

	// connect to server
	for i := 0; i < client_total; i++ {
		addr, err := net.ResolveTCPAddr("tcp4", cfg_test_game_server_addr)
		if err != nil {
			fmt.Printf("ResolveTCPAddr [err = %v]\n", err)
			return
		}
		conn, err := net.DialTCP("tcp", nil, addr)
		if err != nil {
			fmt.Printf("DialTCP [err = %v]\n", err)
			return
		} else {
			fmt.Printf("dial ok. [local addr = %s]\n", conn.LocalAddr().String())

			conn.SetNoDelay(true)
			conn.SetKeepAlive(true)
			conn.SetReadBuffer(int(pb.Constant_Message_MaxBody_Len+pb.Constant_Message_Head_Len) * 10)
			conn.SetWriteBuffer(int(pb.Constant_Message_MaxBody_Len+pb.Constant_Message_Head_Len) * 10)

			var c Client
			c.conn = conn
			c.step = Step_ToAuth
			c.recvFlag = true
			c.recvBuffer = make([]byte, int(pb.Constant_Message_MaxBody_Len+pb.Constant_Message_Head_Len)*10)
			c.sendBuffer = make([]byte, int(pb.Constant_Message_MaxBody_Len+pb.Constant_Message_Head_Len)*10)
			c.recvState = RecvState_Head
			c.sendCh = make(chan bool, 2)

			wg.Add(2)
			counterClients++
			counterClients++
			c.conn.SetDeadline(time.Time{})
			go client_write_routine(&c, &wg)
			go client_read_routine(&c, &wg)

			time.Sleep(time.Millisecond * 5)
		}
	}

	wg.Wait()

	fmt.Println("game server testing exit.")
	ch <- true
}

func client_read_routine(client *Client, wg *sync.WaitGroup) {
	defer wg.Done()
	defer atomic.AddInt32(&counterClients, -1)
	//defer fmt.Printf("Routine Exit: client_read_routine [addr = %s]\n", client.conn.LocalAddr().String())

	bRemoveSession := false
	for !bRemoveSession {

		// 游戏开始以后，不处理消息(为了排除客户端接收数据不及时的情况)
		if client.step == Step_ToPlay && cfg_drop_game_frame {
			if _, err := client.conn.Read(client.recvBuffer[0:]); err != nil {
				glog.Errorf("读消息失败 [err = %s, addr = %s]\n", err.Error(), client.conn.LocalAddr().String())
				bRemoveSession = true
			}
			client.recvPos, client.recvTotal = 0, 0
			client.lastRecv = time.Now().UnixNano()
			continue
		}

		// read head
		if !bRemoveSession && client.recvState == RecvState_Head {
			n, err := client.conn.Read(client.recvBuffer[0:pb.Constant_Message_Head_Len])
			if err != nil {
				glog.Errorf("读消息头失败 [err = %s, addr = %s]\n", err.Error(), client.conn.LocalAddr().String())
				bRemoveSession = true
			} else if n < int(pb.Constant_Message_Head_Len) {
				glog.Errorf("读消息头失败，没有一次读完 [n = %d, need_n, addr = %s]\n", n, pb.Constant_Message_Head_Len, client.conn.LocalAddr().String())
				bRemoveSession = true
			} else {
				if err := proto.Unmarshal(client.recvBuffer[0:int(pb.Constant_Message_Head_Len)], &client.recvMsgHead); err != nil {
					glog.Errorf("消息头解析失败 [err = %s, addr = %s]\n", err.Error(), client.conn.LocalAddr().String())
					bRemoveSession = true
				} else {
					client.recvTotal = 0
					client.recvState = RecvState_Body
				}
				client.lastRecv = time.Now().UnixNano()
			}
		}
		// read body
		if !bRemoveSession && client.recvState == RecvState_Body {
			if client.recvMsgHead.BodyLen >= int32(pb.Constant_Message_MaxBody_Len) {
				fmt.Printf("包体长度过大 [body len(%d) > %d, addr = %s]\n", client.recvMsgHead.BodyLen, int32(pb.Constant_Message_MaxBody_Len), client.conn.LocalAddr().String())
				bRemoveSession = true
			} else {

				// read body
				if client.recvMsgHead.BodyLen > 0 {
					n, err := client.conn.Read(client.recvBuffer[0:client.recvMsgHead.BodyLen])
					if err != nil {
						glog.Errorf("读消息体失败 [err = %s, addr = %s]\n", err.Error(), client.conn.LocalAddr().String())
						bRemoveSession = true
					} else if n < int(client.recvMsgHead.BodyLen) {
						glog.Errorf("读消息体失败，没有一次读完 [n = %d, need_n, addr = %s]\n", n, client.recvMsgHead.BodyLen, client.conn.LocalAddr().String())
						bRemoveSession = true
					}
				}

				// process
				if !bRemoveSession {
					if client.recvMsgHead.BodyLen < 0 {
						client.recvMsgHead.BodyLen = 0
					}

					//fmt.Printf("收到消息 [msg_id = %d, msg_bodylen = %d, from = %s]\n", client.recvMsgHead.MessageId, client.recvMsgHead.BodyLen, client.conn.RemoteAddr().String())
					if ok, err := messageHandle(client.recvMsgHead.MessageId, client.recvBuffer[0:client.recvMsgHead.BodyLen], client); !ok {
						glog.Errorf("消息处理失败 [err = %s, msg_id = %d, msg_bodylen = %d, from = %s]\n", err, client.recvMsgHead.MessageId, client.recvMsgHead.BodyLen, client.conn.LocalAddr().String())
						bRemoveSession = true
					}

					client.recvTotal = 0
					client.recvState = RecvState_Head
					client.lastRecv = time.Now().UnixNano()
				}
			}
		}
	}

	client.sendCh <- false
}

func client_write_routine(client *Client, wg *sync.WaitGroup) {
	defer wg.Done()
	defer atomic.AddInt32(&counterClients, -1)
	defer fmt.Printf("Routine Exit: client_write_routine [addr = %s]\n", client.conn.LocalAddr().String())

	// construct a new request
	messageMake(client)

	bRemoveSession := false
	for !bRemoveSession {

		//fmt.Println("client_write_routine loop")

		go_on := <-client.sendCh
		if !go_on {
			break
		}

		//fmt.Println("client_write_routine write")

		// write data to socket from buffer
		if client.sendPos < client.sendTotal {
			//client.conn.SetWriteDeadline(time.Now().Add(time.Second * 10))
			n, err := client.conn.Write(client.sendBuffer[client.sendPos:client.sendTotal])
			if err != nil {
				glog.Errorf("发送消息失败 [err = %s, addr = %s]\n", err, client.conn.LocalAddr().String())
				bRemoveSession = true
			} else if n > 0 {
				//if n != client.sendTotal-client.sendPos {
				//	fmt.Printf("数据没有一次发送完成 [total = %d, n = %d, addr = %s]\n", client.sendTotal-client.sendPos, n, client.conn.LocalAddr().String())
				//}
				client.sendPos = client.sendPos + n
				if client.sendPos >= client.sendTotal {
					client.sendPos, client.sendTotal = 0, 0
				}
				//fmt.Printf("send to data. [n = %d, addr = %s]\n", n, client.conn.RemoteAddr().String())

				if client.lastSend != 0 {
					if delta := time.Now().UnixNano() - client.lastSend; delta > int64(time.Millisecond)*335 {
						//fmt.Println("interval of send: ", delta/int64(time.Millisecond))
					}
				}
				client.lastSend = time.Now().UnixNano()
			}
		} else {
			client.sendPos, client.sendTotal = 0, 0
		}
	}

	client.conn.Close()
	if client.ticker != nil {
		client.ticker.Stop()
	}
}

func client_simulator() {
	// 客户端模拟程序
	fmt.Println("please input ip, number of clients, role id, room type. for example: test.exe 192.168.0.129:6000 9 3 4")
	if len(os.Args) < 5 {
		cfg_test_game_server_addr = "0.0.0.0:6000"
		cfg_client_total = 10
	} else {
		cfg_test_game_server_addr = os.Args[1]
		cfg_client_total, _ = strconv.Atoi(os.Args[2])
		cfg_role_id, _ = strconv.Atoi(os.Args[3])
		room_type, _ := strconv.Atoi(os.Args[4])
		cfg_room_type = uint32(room_type)
	}

	test_game_server(cfg_client_total)
}

func init() {
	cfg_log_dir := common.GetAbsFilePath(cfg_log_dir)
	os.MkdirAll(cfg_log_dir, 0777)
	flag.Set("log_dir", cfg_log_dir)
	flag.Parse()

	strconv.Atoi("1")
}

type ranking_action struct {
	param *pb.Player
}

func test_rankinglist() {
	rankingList := pb.RankingList{}

	for i := 0; i < 10; i++ {
		action := ranking_action{param: &pb.Player{PlayerId: (uint64)(i), Nickname: "dodo", Exp: int32(i)}}
		insert_ranking(&rankingList, &action)
	}

	insert_ranking(&rankingList, &ranking_action{param: &pb.Player{PlayerId: (uint64)(345), Nickname: "dodo", Exp: int32(100)}})
	insert_ranking(&rankingList, &ranking_action{param: &pb.Player{PlayerId: (uint64)(344), Nickname: "dodo", Exp: int32(5)}})
	insert_ranking(&rankingList, &ranking_action{param: &pb.Player{PlayerId: (uint64)(345), Nickname: "dodo", Exp: int32(200)}})
	//insert_ranking(&rankingList, &ranking_action{param: &pb.Player{PlayerId: (uint64)(2423), Nickname: "dodo", Exp: int32(6)}})
	//insert_ranking(&rankingList, &ranking_action{param: &pb.Player{PlayerId: (uint64)(56546), Nickname: "dodo", Exp: int32(30)}})

	fmt.Printf("ranking list: \n")
	for i, v := range rankingList.Players {
		fmt.Printf("player(%d): %v\n", i, v)
	}
}

func insert_ranking(rankingList *pb.RankingList, action *ranking_action) {
	pos_new, pos_old := -1, -1
	total := len(rankingList.Players)

	fmt.Printf("param = %v\n", action.param)
	for i, v := range rankingList.Players {
		if action.param.PlayerId == v.PlayerId {
			pos_old = i
		}
		if pos_new == -1 && v.Exp <= action.param.Exp {
			pos_new = i
		}
	}
	if pos_old != -1 && pos_old+1 == pos_new {
		rankingList.Players[pos_old] = action.param
		return
	}

	fmt.Printf("pos_new = %d, pos_old = %d\n", pos_new, pos_old)

	if pos_old != -1 {
		fmt.Println("pos_old 1")
		if pos_old+1 < total {
			rankingList.Players = append(rankingList.Players[:pos_old], rankingList.Players[pos_old+1:]...)
		} else {
			rankingList.Players = append(rankingList.Players[:pos_old], []*pb.Player{}...)
		}
	}
	if pos_new == -1 {
		rankingList.Players = append(rankingList.Players, action.param)
		fmt.Println("pos_new -1")
	} else {
		rear := append([]*pb.Player{}, rankingList.Players[pos_new:]...)
		rankingList.Players = append(rankingList.Players[:pos_new], action.param)
		rankingList.Players = append(rankingList.Players, rear...)
		fmt.Println("pos_new 1")
	}
}

func test() {
	//ticker := time.NewTicker(time.Second / time.Duration(60))
	ticker := time.NewTicker(time.Millisecond * 30)
	defer ticker.Stop()

	last_t := time.Now().UnixNano()
	for range ticker.C {
		b_time := time.Now().UnixNano()

		delta := b_time - last_t
		last_t = b_time

		if delta/int64(time.Millisecond) > 33 {
			fmt.Printf("room tick. [delta = %d(ms)]\n", delta/int64(time.Millisecond))
		}
	}
}

func main() {
	defer glog.Flush()

	ch = make(chan bool, 1)

	//test_account_server()
	//test_account_server2()
	//test_db_server()
	//client_simulator()
	test_game_server(cfg_client_total)
	//test_rankinglist()

	//for i := 0; i < 10000; i++ {
	//	go test()
	//}

	<-ch
}
