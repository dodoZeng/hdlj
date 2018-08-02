package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"roisoft.com/hdlj/common"
	pb "roisoft.com/hdlj/proto"
)

type IMessage interface {
	RequestFromBuffer([]byte) (bool, string)
	ReplyToBuffer() []byte
	Handle(*Session) (bool, string)
	GetMessageId(bool) uint32
}

type MessageFactory func() IMessage

var gMsgFactory map[pb.CS_MessageId]MessageFactory

// 初始化全局变量
func init() {
	gMsgFactory = make(map[pb.CS_MessageId]MessageFactory)

	gMsgFactory[pb.CS_MessageId_C2S_Signin_GameServer] = func() IMessage { return &MsgSigninGameserver{} }
	gMsgFactory[pb.CS_MessageId_C2S_Start_PlayerMatching] = func() IMessage { return &MsgPlayerMatching{} }
	gMsgFactory[pb.CS_MessageId_C2S_Cancel_PlayerMatching] = func() IMessage { return &MsgPlayerMatchingCancel{} }
	gMsgFactory[pb.CS_MessageId_C2S_Sync_LoadingProgress] = func() IMessage { return &MsgLoadingProgress{} }
	gMsgFactory[pb.CS_MessageId_C2S_Sync_PlayerOperation] = func() IMessage { return &MsgPlayerCommands{} }
	gMsgFactory[pb.CS_MessageId_C2S_Sync_Gameover] = func() IMessage { return &MsgGameover{} }
	gMsgFactory[pb.CS_MessageId_C2S_Get_ServerTimestamp] = func() IMessage { return &MsgGetTimestamp{} }
	gMsgFactory[pb.CS_MessageId_C2S_Synchronize_Time_Request] = func() IMessage { return &MsgPing{} }
	gMsgFactory[pb.CS_MessageId_C2S_Modify_Nickname_Request] = func() IMessage { return &MsgNickname{} }
	gMsgFactory[pb.CS_MessageId_C2S_Sync_StatusCheckResult] = func() IMessage { return &MsgStatusCheckResult{} }
	gMsgFactory[pb.CS_MessageId_C2S_Set_RoleSettings_Request] = func() IMessage { return &MsgSetRoleSetting{} }
	gMsgFactory[pb.CS_MessageId_C2S_Hall_Operation_Request] = func() IMessage { return &MsgHallOperation{} }
	gMsgFactory[pb.CS_MessageId_C2C_Multicast] = func() IMessage { return &MsgForward{} }
	gMsgFactory[pb.CS_MessageId_C2S_Rangking_Request] = func() IMessage { return &MsgRankingList{} }

}

func new_dbplayer(player_id uint64) *pb.DbGameData {
	return &pb.DbGameData{PlayerId: player_id, CountersDay: make([]int32, pb.CounterEnum_CounterEnum_Max), Game: &pb.GameData{PlayerProperties: make([]int32, pb.PropertyId_PropertyId_Max)}}
}

// 发送消息给指定玩家
func SendMessage(playerid uint64, buf []byte) bool {
	return true
}

// ------------------------------------ 公共消息
type MsgCommon struct {
	reply_message_id pb.CS_MessageId
	reply_buf        []byte
}

func (pThis *MsgCommon) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pThis.reply_message_id)
	} else {
		return 0
	}
}

func (pThis *MsgCommon) RequestFromBuffer(buf []byte) (bool, string) {
	return true, ""
}

func (pThis *MsgCommon) ReplyToBuffer() []byte {
	return pThis.reply_buf
}

func (pThis *MsgCommon) Handle(s *Session) (bool, string) {
	return true, ""
}

// ------------------------------------ 登录
type MsgSigninGameserver struct {
	request pb.SigninRequest
	reply   pb.ErrorInfo
}

func (pThis *MsgSigninGameserver) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Signin_GameServer_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Signin_GameServer)
	}
}

func (pThis *MsgSigninGameserver) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgSigninGameserver) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}

	return buf
}

func (pThis *MsgSigninGameserver) Handle(s *Session) (bool, string) {

	pThis.reply.Code = int32(pb.ErrorCode_CS_UNKNOW)
	pThis.reply.Context = "invalid parameter."

	is_newer_player := false
	for i := 0; i < 1; i++ {
		if pThis.request.PlayerId == 0 || pThis.request.SessionId == "" {
			return false, "无效的SessioinId"
		}

		// 加载游戏数据
		if pThis.request.PlayerId < robot_playerid { // 暂时（方便机器人登录）
			// Session合法性检查
			master := masterCluster.GetNodeByPlayerId(pThis.request.PlayerId)
			if master == nil {
				pThis.reply.Code = int32(pb.ErrorCode_CS_UNKNOW)
				pThis.reply.Context = "master is not available"
				break
			}
			ctx := context.Background()
			if cfg_grpc_client_timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
				defer cancel()
			}
			request := &pb.SessionRequest{SessionId: pThis.request.SessionId, PlayerId: pThis.request.PlayerId, GameServerId: cfg_node_id}
			if r, err := master.CheckSession(ctx, request); err != nil {
				pThis.reply.Code = int32(pb.ErrorCode_CS_UNKNOW)
				pThis.reply.Context = fmt.Sprintf("%v", err)
				break
			} else {
				if r.Result.Code != int32(pb.ErrorCode_CS_OK) {
					pThis.reply.Code = r.Result.Code
					pThis.reply.Context = r.Result.Context
					break
				}
			}

			// 多端同时登录检测
			if tmp, ok := playerManager.Load(pThis.request.PlayerId); ok {
				old_s := tmp.(*Session)
				// 多端同时登录同一账号
				if old_s.conn != nil && old_s.connState != ConnState_Dead && old_s.connState != ConnState_WaitingForClose {
					glog.Infof("Player signin again. [player_id = %d, addr = %s]\n", old_s.playerInfo.playerId, old_s.conn.RemoteAddr().String())
					old_s.Free()
				}
				fmt.Printf("load game data from playerManager. [player_id = %d]\n", pThis.request.PlayerId)
				s.playerInfo = old_s.playerInfo
				// 已经在游戏中
				if s.playerInfo.room != nil {
					if s.playerInfo.playerState == PlayerState_Playing && s.playerInfo.room.state == RoomState_Playing {

					} else {
						s.playerInfo.room = nil
					}
				}
			}

			// 加载游戏数据
			if s.playerInfo.dbData == nil {
				dbGameData := (*pb.DbGameData)(nil)

				redis_is_ok := true
				if err := redisRead.Ping().Err(); err != nil {
					redis_is_ok = false

					// reconn to redis
					if new_c, _ := getRedisClient(); new_c != nil {
						redisRead = new_c
						redis_is_ok = true
					}
				}

				if !redis_is_ok {
					glog.Errorf("redis client is unreachable. [playerid = %d, node_id = %s]\n", pThis.request.PlayerId, cfg_node_id)
					pThis.reply.Context = "redis client is unreachable."
					break
				} else {
					key_redis := fmt.Sprintf(redis_player_format, pThis.request.PlayerId)
					in_redis := false

					// 判断是否有Redis数据缓存
					if result, err := redisRead.Exists(key_redis).Result(); err != nil {
						glog.Errorf("redis error. [playerid = %d, node_id = %s, err = %v]\n", pThis.request.PlayerId, cfg_node_id, err)
						pThis.reply.Context = "redis error."
						break
					} else {
						//fmt.Printf("redis exist: %d\n", result)
						if result > 0 {
							in_redis = true
						}
					}

					if !in_redis {
						fmt.Printf("load game data from db. [player_id = %d]\n", pThis.request.PlayerId)

						// 从数据库加载游戏数据
						db := dbCluster.GetNodeByPlayerId(pThis.request.PlayerId)
						if db == nil {
							glog.Errorf("dbproxy is unreachable. [playerid = %d, node_id = %s]\n", pThis.request.PlayerId, cfg_node_id)
							pThis.reply.Context = "db proxy is unreachable."
							break
						}
						key_db := pb.KeyPlayerId{Key: pThis.request.PlayerId}
						key_buf, _ := proto.Marshal(&key_db)
						dbrequest := &pb.DbRequest{Type: uint32(pb.OpType_Game), Method: uint32(pb.OpMethod_GET), Key: key_buf}
						if cfg_grpc_client_timeout > 0 {
							var cancel context.CancelFunc
							ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
							defer cancel()
						}

						if r, err := db.DbOperation(ctx, dbrequest); err != nil {
							glog.Errorf("dbproxy error. [playerid = %d, node_id = %s, err = %v]\n", pThis.request.PlayerId, cfg_node_id, err)
							pThis.reply.Context = "dbproxy error."
							break
						} else {
							// 玩家数据不存在，则表示是新玩家
							if r.GetValue() == nil {
								fmt.Printf("this is a newer player. [playerid = %d]\n", pThis.request.PlayerId)
								dbGameData = new_dbplayer(pThis.request.PlayerId)
								is_newer_player = true
							} else {
								fmt.Printf("game data = %v\n", r.GetValue())

								dbObj := pb.DbGameData{}
								if err := proto.Unmarshal(r.Value, &dbObj); err != nil {
									glog.Errorf("DbGameData unmarshal error. [playerid = %d, error = %v]\n", pThis.request.PlayerId, err)
									pThis.reply.Context = "DbGameData Unmarshal error."
									break
								} else {
									dbGameData = &dbObj
								}
							}
						}
					} else {
						fmt.Printf("load game data from redis. [player_id = %d]\n", pThis.request.PlayerId)

						// 从本地缓存加载游戏数据
						if redis_buf, err := redisRead.Get(key_redis).Bytes(); err != nil {
							glog.Errorf("redis error. [playerid = %d, node_id = %s, err = %v]\n", pThis.request.PlayerId, cfg_node_id, err)
							pThis.reply.Context = "redis error."
							break
						} else {
							var redis_cache_data pb.RGameDataInfo
							if err := proto.Unmarshal(redis_buf, &redis_cache_data); err != nil {
								glog.Errorf("RGameDataInfo unmarshal error. [playerid = %d, error = %v]\n", pThis.request.PlayerId, err)
								pThis.reply.Context = "RGameDataInfo Unmarshal error."
								break
							} else {
								dbGameData = redis_cache_data.Data
							}
						}
					}
				}

				s.playerInfo.dbData = dbGameData
			}

		} else {
			s.playerInfo.dbData = new_dbplayer(pThis.request.PlayerId)
		}

		// 加载游戏数据失败
		if s.playerInfo.dbData == nil {
			pThis.reply.Code = int32(pb.ErrorCode_CS_UNKNOW)
			pThis.reply.Context = fmt.Sprintf("fail to load game data. [player_id = %d, session_id = %s]\n", pThis.request.PlayerId, pThis.request.SessionId)
			break
		}

		// 登录成功
		s.connState = ConnState_Auth
		s.playerInfo.playerId = pThis.request.PlayerId
		s.playerInfo.sessionId = pThis.request.SessionId

		// 测试账号
		/*
			switch s.playerInfo.playerId {
			case 100065: //01
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 0
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 0
			case 100066: //02
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 0
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 0
			case 100067: //03
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 0
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 0
			case 100068: //04
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 99999
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 99999
			case 100113: //05
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 99999
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 99999
			case 100114: //06
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 99999
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 99999
			case 100123: //07
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 4000
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 4000
			case 100124: //08
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 4000
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 4000
			case 100125: //09
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 4000
				s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 4000
			}
		*/

		// 累计在线时间
		now_date := time.Now()
		now := now_date.UnixNano()
		last_time := time.Unix(s.playerInfo.dbData.CountersLastTime/int64(time.Second), s.playerInfo.dbData.CountersLastTime%int64(time.Second))
		zero_counter := s.playerInfo.dbData.CountersLastTime == 0 || now_date.Year()-last_time.Year() > 0 || now_date.Month()-last_time.Month() > 0 || now_date.Day()-last_time.Day() > 0
		if s.playerInfo.dbData.Game.OnlineLastTime == 0 {
			s.playerInfo.dbData.Game.OnlineLastTime = now
		}
		// 计数器清零
		if zero_counter {
			s.playerInfo.dbData.CountersDay = make([]int32, pb.CounterEnum_CounterEnum_Max)
			s.playerInfo.dbData.CountersLastTime = now
			// 在黑市商店产生三个没有的装备
			s.makeBlackStoreEquip()
			s.makeDailyAward()
		}
		// 防沉迷
		offline_acc := (now - s.playerInfo.dbData.Game.OnlineLastTime) / int64(time.Second)
		if offline_acc >= int64(time.Hour)*5 {
			s.playerInfo.dbData.Game.OnlineAccumulateCm = 0
		}
		s.playerInfo.dbData.Game.OnlineLastTime = now
		// 恢复玩家状态
		if s.playerInfo.playerState != PlayerState_Playing {
			s.playerInfo.playerState = PlayerState_InHall
			s.playerInfo.matching_room_type = 0
			s.playerInfo.room = nil
		}
		// 新玩家奖励
		if is_newer_player {
			// 暂时
			s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = 99999
			s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_soul_coin] = 1000

			// 送两个英雄
			s.playerInfo.dbData.Game.Heroes = append(s.playerInfo.dbData.Game.Heroes, &pb.Hero{HeroId: 9009005, Level: 1}) // 祝融
			s.playerInfo.dbData.Game.Heroes = append(s.playerInfo.dbData.Game.Heroes, &pb.Hero{HeroId: 9009006, Level: 1}) // 井工

			// 初始装备
		}
		// 玩家排行
		chRankingAction <- &ranking_action{action_id: RankingAction_Add, s: s, param: &pb.Player{PlayerId: s.playerInfo.playerId, Nickname: s.playerInfo.dbData.Game.Nickname, Icon: s.playerInfo.dbData.Game.Icon, Exp: s.playerInfo.dbData.Game.PlayerProperties[pb.PropertyId_id_player_exp]}}

		s.setSavingRequirement()
		playerManager.Store(pThis.request.PlayerId, s)
		atomic.AddInt32(&total_player, 1)

		pThis.reply.Code = int32(pb.ErrorCode_CS_OK)
		pThis.reply.Context = ""
	}

	// 返回处理结果
	s.PostMessage(pThis)

	// 登录失败断开连接
	if pThis.reply.Code != int32(pb.ErrorCode_CS_OK) {
		glog.Infof("玩家登录不成功. [player_id = %d, session_id = %s, error = %s, addr = %s]\n", pThis.request.PlayerId, pThis.request.SessionId, pThis.reply.Context, s.conn.RemoteAddr().String())
		return false, pThis.reply.Context
	} else {
		glog.V(common.Log_Info_Level_1).Infof("Player signin gameserver successfully. [player_id = %d, session_id = %s, addr = %s]\n", pThis.request.PlayerId, pThis.request.SessionId, s.conn.RemoteAddr().String())
		fmt.Printf("玩家登录成功. [player_id = %d, session_id = %s, addr = %s]\n", pThis.request.PlayerId, pThis.request.SessionId, s.conn.RemoteAddr().String())

		// 同步玩家游戏数据给客户端
		gamedata_buf, _ := proto.Marshal(s.playerInfo.dbData.Game)
		s.PostMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Sync_Player_GameData, reply_buf: gamedata_buf})
		// 发送推送消息
		pushingManager.Range(func(k interface{}, v interface{}) bool {
			msg := v.(*pushing_message)
			now := time.Now().UnixNano()
			if now >= msg.ts_from && now <= msg.ts_to {
				fmt.Printf("pushing message to player. [player_id = %d]\n", s.playerInfo.playerId)

				msg_body := pb.PushingMessage{Text: msg.text, MsgType: uint32(msg.msg_type), TimeStamp: now}
				msg_buf, _ := proto.Marshal(&msg_body)
				s.PostMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Pushing_Message, reply_buf: msg_buf})
			}
			return true
		})
	}

	return true, ""
}

// ------------------------------------ 玩家匹配
type MsgPlayerMatching struct {
	request pb.PlayerMatchingRequest
	reply   pb.ErrorInfo
}

func (pThis *MsgPlayerMatching) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Start_PlayerMatching_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Start_PlayerMatching)
	}
}

func (pThis *MsgPlayerMatching) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgPlayerMatching) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}

	return buf
}

func (pThis *MsgPlayerMatching) Handle(s *Session) (bool, string) {

	fmt.Printf("收到玩家匹配请求 [player = %d]\n", s.playerInfo.playerId)

	// 暂时修改
	//s.playerInfo.playerState = PlayerState_InHall

	// 检查玩家当前状态
	if s.playerInfo.playerState != PlayerState_InHall && s.playerInfo.playerState != PlayerState_GameOver {
		pThis.reply.Code = int32(pb.ErrorCode_CS_INVALID_PLAYERSTATE)
		pThis.reply.Context = "当前玩家状态不正确"

		fmt.Printf("玩家匹配失败 [player = %d, player_state = %d, err = %s]\n", s.playerInfo.playerId, s.playerInfo.playerState, pThis.reply.Context)
		s.PostMessage(pThis)
		return true, pThis.reply.Context
	}

	// 检查房间类型是否正确
	if pThis.request.RoomType <= uint32(pb.RoomType_RoomType_Void) || pThis.request.RoomType >= uint32(pb.RoomType_RoomType_Max) {
		pThis.reply.Code = int32(pb.ErrorCode_CS_INVALID_PARAMETER)
		pThis.reply.Context = "房间类型不正确"

		fmt.Printf("玩家匹配失败 [player = %d, room_type = %d, err = %s]\n", s.playerInfo.playerId, pThis.request.RoomType, pThis.reply.Context)
		s.PostMessage(pThis)
		return true, pThis.reply.Context
	}

	chPlayerMatching[pThis.request.RoomType] <- &MatchingAction{s: s, action: MatchingAction_ADD}

	return true, pThis.reply.Context
}

// ------------------------------------ 玩家取消匹配
type MsgPlayerMatchingCancel struct {
	reply pb.ErrorInfo
}

func (pThis *MsgPlayerMatchingCancel) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Cancel_PlayerMatching_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Cancel_PlayerMatching)
	}
}

func (pThis *MsgPlayerMatchingCancel) RequestFromBuffer(buf []byte) (bool, string) {

	return true, ""
}

func (pThis *MsgPlayerMatchingCancel) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}

	return buf
}

func (pThis *MsgPlayerMatchingCancel) Handle(s *Session) (bool, string) {

	fmt.Printf("收到玩家取消匹配请求 [player = %d]\n", s.playerInfo.playerId)

	if s.playerInfo.playerId <= 0 {
		return false, "not signin."
	}

	// 检查玩家当前状态
	if s.playerInfo.playerState != PlayerState_Matching {
		pThis.reply.Code = int32(pb.ErrorCode_CS_INVALID_PLAYERSTATE)
		pThis.reply.Context = "当前玩家状态为匹配中，取消匹配失败"
		fmt.Printf("取消匹配失败 [player = %d, err = %s]\n", s.playerInfo.playerId, pThis.reply.Context)
		s.PostMessage(pThis)
	} else {
		chPlayerMatching[s.playerInfo.matching_room_type] <- &MatchingAction{s: s, action: MatchingAction_REM}
	}

	return true, ""
}

// ------------------------------------ 场景加载进度
type MsgLoadingProgress struct {
	request pb.PlayerState
	reply   pb.PlayerState
}

func (pThis *MsgLoadingProgress) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Sync_LoadingProgress)
	} else {
		return uint32(pb.CS_MessageId_C2S_Sync_LoadingProgress)
	}
}

func (pThis *MsgLoadingProgress) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgLoadingProgress) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}
	return buf
}

func (pThis *MsgLoadingProgress) Handle(s *Session) (bool, string) {

	if s.playerInfo.playerState == PlayerState_InRoom && s.playerInfo.room != nil {
		// 广播给其他玩家
		pThis.reply = pThis.request
		pThis.reply.PlayerId = s.playerInfo.playerId
		BroadcastMessage(pThis, &s.playerInfo.room.players, s.playerInfo.playerId, true)

		if pThis.request.Percent >= 100 {
			//fmt.Printf("发送加载进度action到房间 [player = %d]\n", s.playerInfo.playerId)
			s.playerInfo.playerState = PlayerState_Prepared
			s.playerInfo.roleId = pThis.request.RoleId
			s.playerInfo.teamId = pThis.request.TeamId

			if len(s.playerInfo.room.ch) >= RoomActionQueueMaxNum {
				glog.Infof("room queue full. [room = %d, max = %d]\n", s.playerInfo.room.roomId, RoomActionQueueMaxNum)
			} else {
				fmt.Printf("收到玩家加载进度完成 [player = %d]\n", s.playerInfo.playerId)
				s.playerInfo.room.ch <- &RoomAction{action: RoomAction_Player_LoadingOver, frame_command: nil, player: s.playerInfo.playerId}
			}
		}
	}

	return true, ""
}

// ------------------------------------ 游戏指令
type MsgPlayerCommands struct {
	request pb.FrameCommand
}

func (pThis *MsgPlayerCommands) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return 0
	} else {
		return uint32(pb.CS_MessageId_C2S_Sync_PlayerOperation)
	}
}

func (pThis *MsgPlayerCommands) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}
	pThis.request.TimeRecv = time.Now().UnixNano()

	return true, ""
}

func (pThis *MsgPlayerCommands) ReplyToBuffer() []byte {

	// 序列化
	return nil
}

func (pThis *MsgPlayerCommands) Handle(s *Session) (bool, string) {

	if s.playerInfo.playerState == PlayerState_Playing && s.playerInfo.room != nil {
		room := s.playerInfo.room
		cmd := pThis.request
		cur_time := time.Now().UnixNano()
		cur_frame_id := uint32((cur_time - room.game_start_time) / room.game_frame_time_per_frame)

		// 帧合法性校验
		delta := int(cmd.FrameId) - int(cur_frame_id)
		delta2 := 0
		if room.game_frame_delta == cfg_frame_delta_ini && delta != 0 {
			room.game_frame_delta = int32(delta)
		} else {
			delta2 = delta - int(room.game_frame_delta)
		}
		//delta2 = 999999

		if !((room.game_frame_deviation_lower == 0 && room.game_frame_deviation_upper == 0) ||
			(delta >= room.game_frame_deviation_lower && delta <= room.game_frame_deviation_upper) ||
			(delta2 >= room.game_frame_deviation_lower && delta2 <= room.game_frame_deviation_upper)) {
			glog.Infof("drop player command. [room = %d, player_id = %d, delta1 = %d, delta2 = %d, cur_frame_id = %d, player_frame_id = %d]\n", room.roomId, s.playerInfo.playerId, delta, delta2, cur_frame_id, cmd.FrameId)
			return true, ""
		}

		// 接收帧指令时间间隔打印
		if int64(delta)*room.game_frame_time_per_frame > int64(time.Millisecond)*100 {
			fmt.Printf("player command is delayed. [room = %d, player_id = %d, delta1 = %d, delta2 = %d, cur_frame_id = %d, player_frame_id = %d]\n", room.roomId, s.playerInfo.playerId, delta, delta2, cur_frame_id, cmd.FrameId)
		}

		// 合法，转发到房间协程处理
		if len(s.playerInfo.room.ch) >= RoomActionQueueMaxNum {
			glog.Infof("room's queue is full. [room = %d, max = %d]\n", s.playerInfo.room.roomId, RoomActionQueueMaxNum)
		} else {
			//fmt.Printf("player input. [player_id = %d, room_id = %d, buf_len = %d]\n", s.playerInfo.playerId, s.playerInfo.room.roomId, len(pThis.request.Commands.Data))
			s.playerInfo.room.ch <- &RoomAction{action: RoomAction_Player_Commands, frame_command: &pThis.request, player: s.playerInfo.playerId}
		}
	}

	return true, ""
}

// ------------------------------------ 游戏结束
type MsgGameover struct {
	request pb.GameResult
}

func (pThis *MsgGameover) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return 0
	} else {
		return uint32(pb.CS_MessageId_C2S_Sync_Gameover)
	}
}

func (pThis *MsgGameover) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgGameover) ReplyToBuffer() []byte {

	// 序列化
	return nil
}

func (pThis *MsgGameover) Handle(s *Session) (bool, string) {

	fmt.Printf("client game over. [player_id = %d, player_state = %d, addr = %s, result = %v]\n", s.playerInfo.playerId, s.playerInfo.playerState, s.conn.RemoteAddr().String(), pThis.request.Results)

	if s.playerInfo.playerState == PlayerState_Playing && s.playerInfo.room != nil && len(pThis.request.Results) == int(matchingCfg[s.playerInfo.room.roomType].playerTotal) {
		s.playerInfo.playerState = PlayerState_GameOver
		s.playerInfo.game_results = &pThis.request

		// 广播给其他玩家
		//pThis.reply.PlayerId = s.playerInfo.playerId
		//BroadcastMessage(pb.CS_MessageId_S2C_Sync_GameOver, pThis.ReplyToBuffer(), s.playerInfo.room.players, s.playerInfo.playerId, false)

		// 转发到房间协程处理
		if len(s.playerInfo.room.ch) >= RoomActionQueueMaxNum {
			glog.Infof("room queue full. [room = %d, max = %d]\n", s.playerInfo.room.roomId, RoomActionQueueMaxNum)
		} else {
			s.playerInfo.room.ch <- &RoomAction{action: RoomAction_Player_Gameover, frame_command: nil, player: s.playerInfo.playerId}
		}
	}

	return true, ""
}

// ------------------------------------ 获取服务器时间戳
type MsgGetTimestamp struct {
	reply pb.ServerInfo
}

func (pThis *MsgGetTimestamp) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Get_ServerTimestamp_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Get_ServerTimestamp)
	}
}

func (pThis *MsgGetTimestamp) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	return true, ""
}

func (pThis *MsgGetTimestamp) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}
	return buf
}

func (pThis *MsgGetTimestamp) Handle(s *Session) (bool, string) {

	pThis.reply.Timestamp = time.Now().UnixNano()
	s.PostMessage(pThis)

	return true, ""
}

// ------------------------------------ 对时处理
type MsgPing struct {
	request pb.PingRequest
	reply   pb.PingReply
}

func (pThis *MsgPing) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Synchronize_Time_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Synchronize_Time_Request)
	}
}

func (pThis *MsgPing) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	var span pb.MessageSpan

	span.Recv = time.Now().UnixNano()
	span.Send = span.Recv
	pThis.reply.ServerSpan = &span

	return true, ""
}

func (pThis *MsgPing) ReplyToBuffer() []byte {

	pThis.reply.ServerSpan.Send = time.Now().UnixNano()

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}
	return buf
}

func (pThis *MsgPing) Handle(s *Session) (bool, string) {
	defer s.PostMessage(pThis)

	pThis.reply.ClientSpan = pThis.request.Client
	pThis.reply.ServerCurTime = time.Now().UnixNano()

	return true, ""
}

// ------------------------------------ 修改昵称
type MsgNickname struct {
	request pb.Player
	reply   pb.ErrorInfo
}

func (pThis *MsgNickname) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Modify_Nickname_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Modify_Nickname_Request)
	}
}

func (pThis *MsgNickname) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgNickname) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}
	return buf
}

func (pThis *MsgNickname) Handle(s *Session) (bool, string) {
	defer s.PostMessage(pThis)

	pThis.reply.Code = int32(pb.ErrorCode_CS_INVALID_PARAMETER)
	pThis.reply.Context = "invalid parameter."

	if pThis.request.PlayerId == s.playerInfo.playerId && len(pThis.request.GetNickname()) > 0 {

		s.playerInfo.dbData.Game.Nickname = pThis.request.Nickname
		s.playerInfo.dbData.Game.Icon = pThis.request.Icon
		s.setSavingRequirement()

		pThis.reply.Code = int32(pb.ErrorCode_CS_OK)
		pThis.reply.Context = ""
	}

	return true, ""
}

// ------------------------------------ 状态同步检测结果
type MsgStatusCheckResult struct {
	request pb.StatusCheckResult
}

func (pThis *MsgStatusCheckResult) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return 0
	} else {
		return uint32(pb.CS_MessageId_C2S_Sync_StatusCheckResult)
	}
}

func (pThis *MsgStatusCheckResult) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgStatusCheckResult) ReplyToBuffer() []byte {

	// 序列化
	return nil
}

func (pThis *MsgStatusCheckResult) Handle(s *Session) (bool, string) {

	if s.playerInfo.playerState == PlayerState_Playing && s.playerInfo.room != nil && pThis.request.GetPlayers() != nil && pThis.request.FrameId != 0 {
		fmt.Printf("收到状态检测结果: player_id = %d, result = %v\n", s.playerInfo.playerId, pThis.request)
		s.playerInfo.room.ch <- &RoomAction{action: RoomAction_StatusCheckResult_Sync, status_check_result: &pThis.request, player: s.playerInfo.playerId}
	}

	return true, ""
}

// ------------------------------------ 大厅操作
type MsgHallOperation struct {
	request pb.HallOperationRequest
	reply   pb.HallOperationReply
}

func (pThis *MsgHallOperation) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Hall_Operation_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Hall_Operation_Request)
	}
}

func (pThis *MsgHallOperation) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgHallOperation) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}
	return buf
}

func (pThis *MsgHallOperation) Handle(s *Session) (bool, string) {
	defer s.PostMessage(pThis)

	pThis.reply.Result = &pb.ErrorInfo{Code: int32(pb.ErrorCode_CS_INVALID_PARAMETER), Context: "invalid parameter."}

	fmt.Println("hall operation: ", pThis.request)

	data := s.playerInfo.dbData
	if pThis.request.OperationId >= 0 {
		switch pb.HallOperationEnum(pThis.request.OperationId) {
		case pb.HallOperationEnum_Normal_Store_Buy_Hero:
			hero := mapHero[uint32(pThis.request.OperationParam1)]
			cost := int32(0)

			// 检查购买英雄是否存在
			if hero == nil || !hero.is_show {
				//fmt.Println("英雄不存在，或不允许购买")
				break
			}
			// 检查钱够不够
			if pThis.request.OperationParam2 == int32(pb.PropertyId_id_gold_coin) {
				if data.Game.PlayerProperties[pb.PropertyId_id_gold_coin] < hero.bm_price {
					pThis.reply.Result.Code = int32(pb.ErrorCode_CS_NOT_ENOUGH_MONEY)
					pThis.reply.Result.Context = "not enough money."
					break
				} else {
					cost = hero.bm_price
				}
			} else if pThis.request.OperationParam2 == int32(pb.PropertyId_id_soul_coin) {
				if data.Game.PlayerProperties[pb.PropertyId_id_soul_coin] < hero.bm_price {
					pThis.reply.Result.Code = int32(pb.ErrorCode_CS_NOT_ENOUGH_MONEY)
					pThis.reply.Result.Context = "not enough money."
					break
				} else {
					cost = hero.bm_price
				}
			} else {
				//fmt.Println("货币单位不正确")
				break
			}
			// 检查自己是否已经拥有此英雄
			if s.hadHero(hero.hero_id) {
				pThis.reply.Result.Code = int32(pb.ErrorCode_CS_HERO_EXIST)
				pThis.reply.Result.Context = "hero exist."
				break
			}

			fmt.Printf("英雄购买成功. [hero_id = %d, player_id = %d]\n", uint32(pThis.request.OperationParam1), s.playerInfo.playerId)
			pThis.reply.Result.Code = int32(pb.ErrorCode_CS_OK)
			pThis.reply.Result.Context = ""

			// 执行购买操作
			s.addHero(hero.hero_id)
			s.addProperty(uint32(pThis.request.OperationParam2), -cost)
			s.setSavingRequirement()
			// 写入日志
		case pb.HallOperationEnum_Normal_Store_Buy_Item:
			item := mapItem[uint32(pThis.request.OperationParam1)]
			cost := int32(0)

			// 检查购买道具是否存在
			if item == nil {
				//fmt.Println("道具不存在")
				break
			}
			// 检查钱够不够
			if pThis.request.OperationParam2 == int32(pb.PropertyId_id_gold_coin) {
				if data.Game.PlayerProperties[pb.PropertyId_id_gold_coin] < item.gold_price {
					pThis.reply.Result.Code = int32(pb.ErrorCode_CS_NOT_ENOUGH_MONEY)
					pThis.reply.Result.Context = "not enough money."
					break
				} else {
					cost = item.gold_price
				}
			} else if pThis.request.OperationParam2 == int32(pb.PropertyId_id_soul_coin) {
				if data.Game.PlayerProperties[pb.PropertyId_id_soul_coin] < item.soul_price {
					pThis.reply.Result.Code = int32(pb.ErrorCode_CS_NOT_ENOUGH_MONEY)
					pThis.reply.Result.Context = "not enough money."
					break
				} else {
					cost = item.soul_price
				}
			} else {
				//fmt.Println("货币单位不正确")
				break
			}

			fmt.Printf("道具购买成功. [item_id = %d, player_id = %d]\n", uint32(pThis.request.OperationParam1), s.playerInfo.playerId)
			pThis.reply.Result.Code = int32(pb.ErrorCode_CS_OK)
			pThis.reply.Result.Context = ""

			// 执行购买操作
			s.addItem(item.item_id, 1)
			s.addProperty(uint32(pThis.request.OperationParam2), -cost)
			s.setSavingRequirement()
			// 写入日志

		case pb.HallOperationEnum_Normal_Store_Buy_Equip:
			equip := mapEquip[uint32(pThis.request.OperationParam1)]
			cost := int32(0)

			// 检查购买装备是否存在
			if equip == nil || !equip.is_show {
				//fmt.Println("装备不存在，或不允许购买")
				break
			}
			// 检查是否已经拥有该装备
			if s.isInPack(equip.item_id) {
				pThis.reply.Result.Code = int32(pb.ErrorCode_CS_EQUIP_EXIST)
				pThis.reply.Result.Context = "equip exist."
				break
			}
			// 检查钱够不够
			if pThis.request.OperationParam2 == int32(pb.PropertyId_id_gold_coin) {
				if data.Game.PlayerProperties[pb.PropertyId_id_gold_coin] < equip.bm_price {
					pThis.reply.Result.Code = int32(pb.ErrorCode_CS_NOT_ENOUGH_MONEY)
					pThis.reply.Result.Context = "not enough money."
					break
				} else {
					cost = equip.bm_price
				}
			} else {
				//fmt.Println("货币单位不正确")
				break
			}

			fmt.Printf("购买装备成功. [item_id = %d, player_id = %d]\n", uint32(pThis.request.OperationParam1), s.playerInfo.playerId)
			pThis.reply.Result.Code = int32(pb.ErrorCode_CS_OK)
			pThis.reply.Result.Context = ""

			// 执行购买操作
			s.addItem(equip.item_id, 1)
			s.addProperty(uint32(pb.PropertyId_id_gold_coin), -cost)
			s.setSavingRequirement()
			// 写入日志
		case pb.HallOperationEnum_Black_Store_Buy_Equip:
			equip := mapEquip[uint32(pThis.request.OperationParam1)]
			cost := int32(0)
			black_item := s.getBlackItem(uint32(pThis.request.OperationParam1))

			// 检查购买装备是否存在
			if equip == nil || !equip.is_show {
				//fmt.Println("装备不存在，或不允许购买")
				break
			}
			if black_item == nil || black_item.Deal {
				//fmt.Println("装备不在黑市商店里，或已经购买")
				break
			}
			// 检查钱够不够
			if pThis.request.OperationParam2 == int32(pb.PropertyId_id_gold_coin) {
				if data.Game.PlayerProperties[pb.PropertyId_id_gold_coin] < equip.bm_price {
					pThis.reply.Result.Code = int32(pb.ErrorCode_CS_NOT_ENOUGH_MONEY)
					pThis.reply.Result.Context = "not enough money."
					break
				} else {
					cost = equip.bm_price
				}
			} else {
				//fmt.Println("货币单位不正确")
				break
			}
			// 检查自己是否已经拥有此装备
			if s.isInPack(equip.item_id) {
				pThis.reply.Result.Code = int32(pb.ErrorCode_CS_EQUIP_EXIST)
				pThis.reply.Result.Context = "equip exist."
				break
			}

			fmt.Printf("黑市购买成功. [item_id = %d, player_id = %d]\n", uint32(pThis.request.OperationParam1), s.playerInfo.playerId)
			pThis.reply.Result.Code = int32(pb.ErrorCode_CS_OK)
			pThis.reply.Result.Context = ""

			// 执行购买操作
			black_item.Deal = true
			s.addItem(equip.item_id, 1)
			s.addProperty(uint32(pb.PropertyId_id_gold_coin), -cost)
			s.setSavingRequirement()
			// 写入日志
		case pb.HallOperationEnum_Open_TreasureBox:
			item := mapItem[uint32(pThis.request.OperationParam1)]
			award_hero_id := uint32(0)
			award_item_ids := []uint32{}

			// 检查宝箱是否存在
			if item == nil || !s.isInPack(item.item_id) {
				//fmt.Println("道具不存在，或者不在背包中")
				break
			}

			fmt.Printf("打开宝箱成功. [item_id = %d, player_id = %d]\n", uint32(pThis.request.OperationParam1), s.playerInfo.playerId)
			pThis.reply.Result.Code = int32(pb.ErrorCode_CS_OK)
			pThis.reply.Result.Context = ""

			// 生成开箱物品
			switch item.item_id {
			case 9059000: // 灰色灵石
				award_hero_id, award_item_ids = openTreasureBoxGray()
			case 9059001: // 蓝色灵石
				award_hero_id, award_item_ids = openTreasureBoxBlue()
			case 9059002: // 金色灵石
				award_hero_id, award_item_ids = openTreasureBoxGold()
			}
			pThis.reply.Returns = append(pThis.reply.Returns, &pb.PropertyItem{PropertyId: uint32(pb.PropertyId_id_role), PropertyValue: int32(award_hero_id)})
			pThis.reply.Returns = append(pThis.reply.Returns, &pb.PropertyItem{PropertyId: uint32(pb.PropertyId_id_equip), PropertyValue: int32(award_item_ids[0])})
			pThis.reply.Returns = append(pThis.reply.Returns, &pb.PropertyItem{PropertyId: uint32(pb.PropertyId_id_equip), PropertyValue: int32(award_item_ids[1])})
			s.addItem(item.item_id, -1)
			// 把物品加入背包（如果存在，则兑换成为金币）
			if s.hadHero(award_hero_id) {
				s.addProperty(uint32(pb.PropertyId_id_gold_coin), mapHero[award_hero_id].re_price)
			} else {
				s.addHero(award_hero_id)
			}
			for _, item := range award_item_ids {
				if s.isInPack(item) {
					s.addProperty(uint32(pb.PropertyId_id_gold_coin), mapEquip[item].re_price)
				} else {
					s.addItem(item, 1)
				}
			}
			s.setSavingRequirement()
			// 写入日志
		case pb.HallOperationEnum_Recycle_Item:

		case pb.HallOperationEnum_Use_Item:

		case pb.HallOperationEnum_Get_DailyAward:
			db := s.playerInfo.dbData
			if len(db.Game.DailyAwardItems) > 0 && pThis.request.OperationParam1 >= 0 && pThis.request.OperationParam1 < int32(len(db.Game.DailyAwardItems)) && !db.Game.DailyAwardItems[pThis.request.OperationParam1].Deal {
				item := db.Game.DailyAwardItems[pThis.request.OperationParam1]

				item.Deal = true

				// 领取每日奖励
				switch pb.ItemTypeEnum(item.ItemInfo.ItemId) {
				case pb.ItemTypeEnum_ItemType_Property:
					db.Game.PlayerProperties[item.ItemInfo.ItemId] = db.Game.PlayerProperties[item.ItemInfo.ItemId] + item.ItemInfo.Value
				case pb.ItemTypeEnum_ItemType_Equip:
				case pb.ItemTypeEnum_ItemType_Treasure:
					s.addItem(item.ItemInfo.ItemId, 1)
				}
				s.setSavingRequirement()

				fmt.Printf("领取每日奖励成功. [item = %v, player_id = %d]\n", item, s.playerInfo.playerId)
				pThis.reply.Result.Code = int32(pb.ErrorCode_CS_OK)
				pThis.reply.Result.Context = ""
			}
		}
	}

	return true, ""
}

// ------------------------------------ 保存角色装备
type MsgSetRoleSetting struct {
	request pb.SetRoleSetting
	reply   pb.ErrorInfo
}

func (pThis *MsgSetRoleSetting) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Set_RoleSettings_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Set_RoleSettings_Request)
	}
}

func (pThis *MsgSetRoleSetting) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgSetRoleSetting) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}
	return buf
}

func (pThis *MsgSetRoleSetting) Handle(s *Session) (bool, string) {
	defer s.PostMessage(pThis)

	pThis.reply.Code = int32(pb.ErrorCode_CS_INVALID_PARAMETER)
	pThis.reply.Context = "invalid parameter."

	if pThis.request.RoleId != 0 && pThis.request.Setting != nil {
		db := s.playerInfo.dbData
		for _, h := range db.Game.Heroes {
			if h.HeroId == pThis.request.RoleId {
				is_exist := false
				for _, e := range h.Equips {
					if e.SequenceId == pThis.request.Setting.SequenceId {
						e.Settings = pThis.request.Setting.Settings
						is_exist = true
						break
					}
				}

				if !is_exist {
					h.Equips = append(h.Equips, pThis.request.Setting)
				}
				s.setSavingRequirement()

				pThis.reply.Code = int32(pb.ErrorCode_CS_OK)
				pThis.reply.Context = ""
				break
			}
		}
	}

	return true, ""
}

// ------------------------------------ 转发消息
type MsgForward struct {
	request pb.ForwardBody
}

func (pThis *MsgForward) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_C2C_Multicast)
	} else {
		return 0
	}
}

func (pThis *MsgForward) RequestFromBuffer(buf []byte) (bool, string) {

	// 反序列化
	if err := proto.Unmarshal(buf, &pThis.request); nil != err {
		return false, err.Error()
	}

	return true, ""
}

func (pThis *MsgForward) ReplyToBuffer() []byte {

	// 序列化
	return nil
}

func (pThis *MsgForward) Handle(s *Session) (bool, string) {

	if pThis.request.GetBody() != nil {
		tmp_buf := make([]byte, len(pThis.request.Body))
		copy(tmp_buf, pThis.request.Body)

		for _, pid := range pThis.request.GetPlayers() {
			if v, ok := playerManager.Load(pid); ok {
				ss := v.(*Session)
				if ss != nil {
					ss.PostMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_C2C_Multicast, reply_buf: tmp_buf})
				}
			}
		}
	}
	return true, ""
}

// ------------------------------------ 玩家排行
type MsgRankingList struct {
	reply pb.RankingList
}

func (pThis *MsgRankingList) GetMessageId(isReplyMessageId bool) uint32 {
	if isReplyMessageId {
		return uint32(pb.CS_MessageId_S2C_Rangking_Reply)
	} else {
		return uint32(pb.CS_MessageId_C2S_Rangking_Request)
	}
}

func (pThis *MsgRankingList) RequestFromBuffer(buf []byte) (bool, string) {

	return true, ""
}

func (pThis *MsgRankingList) ReplyToBuffer() []byte {

	// 序列化
	buf, err := proto.Marshal(&pThis.reply)
	if nil != err {
		return nil
	}
	return buf
}

func (pThis *MsgRankingList) Handle(s *Session) (bool, string) {

	fmt.Printf("recv ranking request. [player_id = %d]\n", s.playerInfo.playerId)

	chRankingAction <- &ranking_action{action_id: RankingAction_Get, s: s}

	return true, ""
}
