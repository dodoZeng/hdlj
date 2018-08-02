package main

import (
	"container/list"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"roisoft.com/hdlj/common"
	pb "roisoft.com/hdlj/proto"
)

const (
	RoomState_Matching           = 1
	RoomState_Preparing          = 2
	RoomState_Playing            = 3
	RoomState_GameOver           = 4
	RoomState_NonNormal_GameOver = 5

	RoomAction_Player_Disconn         = 1
	RoomAction_Player_LoadingOver     = 2
	RoomAction_Player_Commands        = 3
	RoomAction_Player_Gameover        = 4
	RoomAction_RoomCancel             = 5
	RoomAction_Frame_Ticker           = 6
	RoomAction_StatusCheckResult_Sync = 7

	MatchingAction_ADD  = 1
	MatchingAction_REM  = 2
	MatchingAction_EXIT = 3

	RoomActionQueueMaxNum = 1024
)

type RoomAction struct {
	action              int
	player              uint64
	frame_command       *pb.FrameCommand
	status_check_result *pb.StatusCheckResult
}

// 房间信息
type RoomInfo struct {
	roomId      uint32
	roomType    uint8
	players     list.List // Session
	players_map map[uint64]*Session
	state       uint8
	ch          chan *RoomAction
	mutex       *sync.RWMutex

	game_start_time           int64
	game_prev_frame_time      int64
	game_last_sync_frame_time int64
	game_all_frame            *list.List //pb.GameFrame
	game_command              []*pb.PlayerCommand
	game_command_buf          []pb.PlayerCommand
	game_command_total        int

	game_frame_num_per_second     int
	game_frame_time_per_frame     int64
	game_frame_file_name          string
	game_frame_file               *os.File
	game_frame_cur_frame_id       uint32
	game_frame_last_sync_frame_id uint32
	game_frame_deviation_upper    int
	game_frame_deviation_lower    int
	game_frame_delta              int32

	status_check_frame_id uint32
	status_check_results  map[uint64]*pb.StatusCheckResult
	status_check_shown    bool
}

type PlayerCommand struct {
	playerId     uint64 // 玩家ID
	frameCommand *pb.FrameCommand
}

type PlayerMatchingConfig struct {
	roomType    uint8
	playerTotal uint8
}

type MatchingAction struct {
	s      *Session
	action int
}

type result_counter struct {
	win  int
	lose int
}

type vote_counter struct {
	counter int
	voters  []uint64
}

var (
	lastRoomId       uint32
	chPlayerMatching []chan *MatchingAction
	matchingCfg      []PlayerMatchingConfig
)

func init() {
	lastRoomId = 0
	matchingCfg = make([]PlayerMatchingConfig, pb.RoomType_RoomType_Max)
	chPlayerMatching = make([]chan *MatchingAction, pb.RoomType_RoomType_Max)

	matchingCfg[uint8(pb.RoomType_MapName_5v5_1)] = PlayerMatchingConfig{roomType: uint8(pb.RoomType_MapName_5v5_1), playerTotal: 1}
	matchingCfg[uint8(pb.RoomType_MapName_5v5_2)] = PlayerMatchingConfig{roomType: uint8(pb.RoomType_MapName_5v5_2), playerTotal: 2}
	matchingCfg[uint8(pb.RoomType_MapName_5v5_3)] = PlayerMatchingConfig{roomType: uint8(pb.RoomType_MapName_5v5_3), playerTotal: 10}
	matchingCfg[uint8(pb.RoomType_MapName_5v5_4)] = PlayerMatchingConfig{roomType: uint8(pb.RoomType_MapName_5v5_4), playerTotal: 4}
}

func (room *RoomInfo) Free() {
	glog.V(common.Log_Info_Level_1).Infof("routine exit: room routine [room_id = %d, room_state = %d]\n", room.roomId, room.state)
	fmt.Printf("routine exit: room routine [room_id = %d, room_state = %d]\n", room.roomId, room.state)

	// 保存游戏数据
	if room.game_frame_file != nil {
		room.game_frame_file.Close()
	}
	// 恢复玩家状态
	for e := room.players.Front(); e != nil; {
		cur_e := e
		e = e.Next()

		s := cur_e.Value.(*Session)
		s.playerInfo.playerState = PlayerState_InHall
		s.playerInfo.matching_room_type = 0
		s.playerInfo.game_results = nil
		s.playerInfo.room = nil
		if s.connState == ConnState_Dead {
			playerManager.Delete(s.playerInfo.playerId)
		}

		room.players.Remove(cur_e)
	}
	// 释放资源
	room.game_all_frame = nil
}

func (s *tcp_service) initRoomTask() {
	for i := uint8(1); i < uint8(pb.RoomType_RoomType_Max); i++ {
		chPlayerMatching[i] = make(chan *MatchingAction, cfg_io_pool_size)
		go s.playerMatchingRoutine(&matchingCfg[i], chPlayerMatching[i])
	}
}

func (s *tcp_service) closeRoomTask() {
	for i := uint8(1); i < uint8(pb.RoomType_RoomType_Max); i++ {
		chPlayerMatching[i] <- &MatchingAction{s: nil, action: MatchingAction_EXIT}
	}
}

func (s *tcp_service) playerMatchingRoutine(cfg *PlayerMatchingConfig, chMatching chan *MatchingAction) {
	s.wg.Add(1)
	atomic.AddInt32(&s.counter, 1)
	defer s.wg.Done()
	defer atomic.AddInt32(&s.counter, -1)
	defer fmt.Printf("routine exit: playerMatchingRoutine. [room_type = %d, room_total = %d]\n", cfg.roomType, cfg.playerTotal)

	curPlayerNum := uint8(0)
	curRoom := (*RoomInfo)(nil)
	roomPlayers := make([]*pb.Player, cfg.playerTotal)

	for act := range chMatching {

		session := act.s

		switch act.action {
		case MatchingAction_EXIT:
			glog.Infof("routine exit: playerMatching routine. [room_type = %d, room_total = %d]\n", cfg.roomType, cfg.playerTotal)
			// 把玩家的状态改为“在大厅”
			if curRoom != nil {
				for e := curRoom.players.Front(); e != nil; e = e.Next() {
					s := e.Value.(*Session)

					s.playerInfo.playerState = PlayerState_InHall
					s.playerInfo.matching_room_type = 0
					s.playerInfo.room = nil
				}
			}
			return
		case MatchingAction_ADD:
			if session != nil {
				if curRoom == nil {
					atomic.AddUint32(&lastRoomId, 1)

					curRoom = new(RoomInfo)
					curRoom.roomId = lastRoomId
					curRoom.roomType = cfg.roomType
					curRoom.players_map = make(map[uint64]*Session)
					curRoom.state = RoomState_Matching
					curRoom.ch = make(chan *RoomAction, RoomActionQueueMaxNum)
					curRoom.mutex = new(sync.RWMutex)
					curRoom.game_frame_cur_frame_id = 0
					curRoom.game_frame_num_per_second = cfg_frame_num_per_second
					curRoom.game_frame_time_per_frame = int64(time.Second) / int64(cfg_frame_num_per_second)
					curRoom.game_frame_deviation_lower = int(cfg_frame_deviation_lower_limit / curRoom.game_frame_time_per_frame)
					curRoom.game_frame_deviation_upper = int(cfg_frame_deviation_upper_limit / curRoom.game_frame_time_per_frame)
					curRoom.status_check_results = make(map[uint64]*pb.StatusCheckResult)
				}
				// 检查是否在列表中
				in_list := false
				for e := curRoom.players.Front(); e != nil; e = e.Next() {
					s := e.Value.(*Session)
					if s.playerInfo.playerId == session.playerInfo.playerId {
						in_list = true
					}
				}
				if !in_list {
					curRoom.players.PushBack(session)
					curPlayerNum++
				}
				// 匹配成功
				fmt.Printf("玩家匹配成功 [player = %d]\n", session.playerInfo.playerId)
				reply_buf, _ := proto.Marshal(&pb.ErrorInfo{Code: int32(pb.ErrorCode_CS_OK)})
				session.PostMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Start_PlayerMatching_Reply, reply_buf: reply_buf})
				session.playerInfo.playerState = PlayerState_Matching
				session.playerInfo.matching_room_type = cfg.roomType

				// 人数足够，开启一个房间
				if curPlayerNum >= cfg.playerTotal {
					var room_info pb.Room

					i := 0
					check_ok := true

					// 检查玩家的连接状态
					for e := curRoom.players.Front(); e != nil; {
						cur_e := e
						e = e.Next()

						s := cur_e.Value.(*Session)
						if s.connState == ConnState_Dead || s.connState == ConnState_WaitingForClose {
							check_ok = false

							// 恢复玩家状态
							s.playerInfo.playerState = PlayerState_InHall
							s.playerInfo.matching_room_type = 0
							if s.connState == ConnState_Dead {
								playerManager.Delete(s.playerInfo.playerId)
							}

							curRoom.players.Remove(cur_e)
							curPlayerNum--
							continue
						}

						seed := rand.Intn(100000)
						s.playerInfo.randSeed = uint32(seed)
						roomPlayers[i] = &pb.Player{PlayerId: s.playerInfo.playerId, Nickname: s.playerInfo.dbData.Game.GetNickname(), RandomSeed: uint32(seed)}
						i++
					}
					if !check_ok {
						continue
					}

					glog.V(common.Log_Info_Level_1).Infof("开启一个房间 [room_id = %d, player_total = %d]\n", curRoom.roomId, cfg.playerTotal)
					fmt.Printf("开启一个房间 [room_id = %d, player_total = %d]\n", curRoom.roomId, cfg.playerTotal)
					curRoom.state = RoomState_Preparing
					go s.roomActionRoutine(curRoom)
					go s.roomTickRoutine(curRoom)

					// 通知玩家创建房间成功
					room_info.RoomId = int32(curRoom.roomId)
					room_info.Players = roomPlayers[0:i]
					room_info.GameFrameNumPerSecond = int32(curRoom.game_frame_num_per_second)
					room_info.RandomSeed = rand.Int31()
					room_info_buf, _ := proto.Marshal(&room_info)
					for e := curRoom.players.Front(); e != nil; e = e.Next() {
						s := e.Value.(*Session)

						curRoom.players_map[s.playerInfo.playerId] = s

						s.playerInfo.playerState = PlayerState_InRoom
						s.playerInfo.room = curRoom
						s.PostMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Create_Room, reply_buf: room_info_buf})
					}

					curPlayerNum = 0
					curRoom = nil
				} else {
					// 广播匹配人数
					buf, _ := proto.Marshal(&pb.PlayerMatchingTotal{Total: int32(curPlayerNum)})
					BroadcastMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Sync_PlayerMatching_Total, reply_buf: buf}, &curRoom.players, 0, true)
				}
			}
		case MatchingAction_REM:
			if session != nil && curRoom != nil {
				reply := pb.ErrorInfo{Code: int32(pb.ErrorCode_CS_INVALID_PLAYERSTATE), Context: "当前玩家状态为匹配中，取消匹配失败"}

				// 取消匹配
				for e := curRoom.players.Front(); e != nil; {
					cur_e := e
					e = e.Next()

					s := cur_e.Value.(*Session)
					if s.playerInfo.playerId == session.playerInfo.playerId {
						reply.Code = int32(pb.ErrorCode_CS_OK)
						reply.Context = ""

						session.playerInfo.playerState = PlayerState_InHall
						session.playerInfo.matching_room_type = 0
						session.playerInfo.room = nil
						if session.connState == ConnState_Dead {
							playerManager.Delete(session.playerInfo.playerId)
						}

						curRoom.players.Remove(cur_e)
						curPlayerNum--
						break
					}
				}

				// 发送取消匹配结果
				reply_buf, _ := proto.Marshal(&reply)
				session.PostMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Cancel_PlayerMatching_Reply, reply_buf: reply_buf})

				// 广播匹配人数
				buf, _ := proto.Marshal(&pb.PlayerMatchingTotal{Total: int32(curPlayerNum)})
				BroadcastMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Sync_PlayerMatching_Total, reply_buf: buf}, &curRoom.players, 0, true)
			}
		}
	}
}

func (s *tcp_service) roomActionRoutine(room *RoomInfo) {

	// process all room action
	for {
		// get room action
		ra := <-room.ch
		if ra == nil {
			break
		}

		// process room action
		switch ra.action {
		case RoomAction_Player_Disconn:
			// 玩家断开连接
			//if room.state == RoomState_Preparing && ra.player > 0 {
			if ra.player > 0 { // 暂时修改：如果有客户端掉线，就结束游戏
				// 解散房间
				glog.V(common.Log_Info_Level_1).Infof("房间在准备中，玩家掉线，房间解散. [room_id = %d]\n", room.roomId)
				fmt.Printf("房间在准备中，玩家掉线，房间解散. [room_id = %d]\n", room.roomId)

				BroadcastMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Destroy_Room, reply_buf: nil}, &room.players, 0, true)
				room.state = RoomState_NonNormal_GameOver
				return
			} else if room.state == RoomState_Playing && ra.player > 0 {
				room.ch <- &RoomAction{action: RoomAction_Player_Gameover, frame_command: nil, player: ra.player}
			}
		case RoomAction_Player_LoadingOver:
			if room.state == RoomState_Preparing {

				// 检查是否所有玩家都加载完毕
				n := uint8(0)
				for e := room.players.Front(); e != nil; e = e.Next() {
					s := e.Value.(*Session)
					if s.playerInfo.playerState == PlayerState_Prepared {
						n++
					}
				}
				// 所有玩家的场景都加载完成
				if n >= matchingCfg[room.roomType].playerTotal {
					room.state = RoomState_Playing
					room.game_frame_cur_frame_id = 0
					room.game_all_frame = list.New()
					room.game_command_total = 0
					room.game_command_buf = make([]pb.PlayerCommand, cfg_frame_max_command_num)
					room.game_command = make([]*pb.PlayerCommand, cfg_frame_max_command_num)
					room.game_start_time = time.Now().UnixNano()
					room.game_last_sync_frame_time = room.game_start_time
					room.game_prev_frame_time = room.game_start_time
					if cfg_room_record_flag {
						room.game_frame_file_name = fmt.Sprintf("%s/room_%d.data", cfg_room_record_dir, room.roomId)
						if fp, err := os.Create(room.game_frame_file_name); err != nil {
							fmt.Printf("fail to create file. [file = %s]\n", room.game_frame_file_name)
						} else {
							room.game_frame_file = fp
						}
					}

					// 通知所有玩家，游戏开始
					var notify pb.GameInfo
					notify.GameStartTime = uint64(room.game_start_time)
					notify_buf, _ := proto.Marshal(&notify)
					for e := room.players.Front(); e != nil; e = e.Next() {
						s := e.Value.(*Session)
						s.playerInfo.playerState = PlayerState_Playing
						s.playerInfo.room.game_frame_delta = cfg_frame_delta_ini
						s.PostMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Start_Game, reply_buf: notify_buf})
					}
					glog.V(common.Log_Info_Level_1).Infof("游戏开始. [room_id = %d]\n", room.roomId)
					fmt.Printf("游戏开始. [room_id = %d, frame_num_per_second = %d]\n", room.roomId, room.game_frame_num_per_second)
				}
			}
		case RoomAction_Player_Commands:
			if room.state == RoomState_Playing && ra.player > 0 && ra.frame_command != nil {
				if room.game_command_total < cfg_frame_max_command_num {
					//fmt.Printf("player command. [player_id = %d, command = %v]\n", ra.player, ra.frame_command.Commands.Data)
					cmd := &room.game_command_buf[room.game_command_total]
					cmd.PlayerId = ra.player
					cmd.Commands = ra.frame_command.Commands
					room.game_command[room.game_command_total] = cmd
					room.game_command_total++
				} else {
					glog.Infof("limit of command total, drop player command. [max_command_num = %d, room = %d]\n", cfg_frame_max_command_num, room.roomId)
				}
			}
		case RoomAction_Player_Gameover:
			if room.state == RoomState_Playing {
				sessions := make([]*Session, matchingCfg[room.roomType].playerTotal)
				counter_map := make(map[uint64]*result_counter)
				n := int(0)
				n_offline := int(0)
				for e := room.players.Front(); e != nil; e = e.Next() {
					s := e.Value.(*Session)
					if s.playerInfo.playerState == PlayerState_GameOver {
						sessions[n] = s
						n++
					} else if s.connState == ConnState_WaitingForClose || s.connState == ConnState_Dead {
						n_offline++
					}
				}
				fmt.Printf("当前房间游戏结束的人数. [room_id = %d, result_total = %d, offline = %d]\n", room.roomId, n, n_offline)

				// 游戏结束
				if n+n_offline >= int(matchingCfg[room.roomType].playerTotal) {
					glog.Infof("游戏结束，房间解散. [room_id = %d]\n", room.roomId)
					fmt.Printf("游戏结束，房间解散. [room_id = %d]\n", room.roomId)

					room.state = RoomState_GameOver

					// 统计游戏结果
					for _, s := range sessions[0:n] {
						for _, v := range s.playerInfo.game_results.Results {
							c := counter_map[v.PlayerId]
							if c == nil {
								c = &result_counter{win: 0, lose: 0}
								counter_map[v.PlayerId] = c
							}

							switch pb.PlayerResultEnum(v.Result) {
							case pb.PlayerResultEnum_Win:
								c.win++
							case pb.PlayerResultEnum_Lose:
								c.lose++
							}
						}
					}
					// 下发游戏结果
					var reply pb.GameResult
					for k, v := range counter_map {
						r := pb.PlayerResult{PlayerId: k}
						db := room.players_map[k].playerInfo.dbData

						if v.win > v.lose {
							r.Result = int32(pb.PlayerResultEnum_Win)

							// 奖励
							cur_gold_day := db.CountersDay[pb.CounterEnum_Counter_Gold_Coin]
							if cur_gold_day < 100 {
								gold := int32(5 + rand.Intn(5))
								r.Returns = append(r.Returns, &pb.PropertyItem{PropertyId: uint32(pb.PropertyId_id_gold_coin), PropertyValue: gold})
								db.CountersDay[pb.CounterEnum_Counter_Gold_Coin] = cur_gold_day + gold
								db.Game.PlayerProperties[pb.PropertyId_id_gold_coin] = db.Game.PlayerProperties[pb.PropertyId_id_gold_coin] + gold
							}
							r.Returns = append(r.Returns, &pb.PropertyItem{PropertyId: uint32(pb.PropertyId_id_player_exp), PropertyValue: 5})
							db.Game.PlayerProperties[pb.PropertyId_id_player_exp] = db.Game.PlayerProperties[pb.PropertyId_id_player_exp] + 5
						} else {
							r.Result = int32(pb.PlayerResultEnum_Lose)

							db.Game.PlayerProperties[pb.PropertyId_id_player_exp] = db.Game.PlayerProperties[pb.PropertyId_id_player_exp] - 5
						}
						reply.Results = append(reply.Results, &r)

						// 存档
						room.players_map[k].setSavingRequirement()
						// 玩家排行
						chRankingAction <- &ranking_action{action_id: RankingAction_Add, s: room.players_map[k], param: &pb.Player{PlayerId: k, Nickname: db.Game.Nickname, Icon: db.Game.Icon, Exp: db.Game.PlayerProperties[pb.PropertyId_id_player_exp]}}
					}
					reply_buf, _ := proto.Marshal(&reply)
					BroadcastMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Sync_GameOver, reply_buf: reply_buf}, &room.players, 0, true)

					return
				}
			}
		case RoomAction_RoomCancel:
			glog.V(common.Log_Info_Level_1).Infof("房间开启超时，房间解散. [room_id = %d]\n", room.roomId)
			fmt.Printf("房间开启超时，房间解散. [room_id = %d]\n", room.roomId)

			// 通知所有玩家，房间解散
			BroadcastMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Destroy_Room, reply_buf: nil}, &room.players, 0, true)
			room.state = RoomState_NonNormal_GameOver
			return
		case RoomAction_StatusCheckResult_Sync:
			if room.state == RoomState_Playing && ra.player > 0 && ra.status_check_result != nil {
				if room.status_check_frame_id == 0 || ra.status_check_result.FrameId <= room.status_check_frame_id {
					room.status_check_frame_id = ra.status_check_result.FrameId
					room.status_check_results[ra.player] = ra.status_check_result
				}
			}
		}
	}
}

func (s *tcp_service) roomTickRoutine(room *RoomInfo) {
	s.wg.Add(1)
	atomic.AddInt32(&s.counter, 1)
	defer s.wg.Done()
	defer atomic.AddInt32(&s.counter, -1)

	if room == nil {
		return
	}

	create_time := time.Now().UnixNano()
	last_tick := create_time
	ticker := time.NewTicker(time.Duration(room.game_frame_time_per_frame))
	defer ticker.Stop()

	for {
		<-ticker.C

		b_time := time.Now().UnixNano()

		// 房间循环时间间隔
		if delta := b_time - last_tick; delta >= int64(time.Millisecond)*40 { //room.game_frame_time_per_frame+int64(time.Millisecond)*5 {
			fmt.Printf("room tick. [delta = %d(ms)]\n", delta/int64(time.Millisecond))
		}
		last_tick = b_time

		//fmt.Printf("roomRoutine: loop [room_id = %d, room_state = %d]\n", room.roomId, room.state)

		if room.state == RoomState_Playing { // && b_time-room.game_prev_frame_time >= room.game_frame_time_per_frame {
			room.game_frame_cur_frame_id++
			room.game_prev_frame_time = b_time

			// check status reslut
			if !room.status_check_shown && room.status_check_frame_id > 0 && room.game_frame_cur_frame_id-room.status_check_frame_id >= 100 {
				room.status_check_shown = true

				// 统计状态检测结果
				//fmt.Printf("room.status_check_results: %v\n", room.status_check_results)
				counter_map := make(map[uint64]*vote_counter)
				for k, v := range room.status_check_results {
					if v.FrameId == room.status_check_frame_id && v.Players != nil {
						for _, p := range v.Players {
							r := counter_map[p]
							if r == nil {
								r = &vote_counter{counter: 1}
							} else {
								r.counter++
							}
							r.voters = append(r.voters, k)
							counter_map[p] = r
						}
					}
				}
				// 显示统计结果
				fmt.Println("              状态不同步的统计报告           ")
				fmt.Println("-------------------------------------------")
				fmt.Printf("room_id = %d, total = %d\n", room.roomId, matchingCfg[room.roomType].playerTotal)
				for k, v := range counter_map {
					fmt.Printf("role_id: %d [player_id = %d, counter = %d]\n", room.players_map[k].playerInfo.roleId, k, v.counter)
					fmt.Printf("voter list: ")
					for _, p := range v.voters {
						fmt.Printf("%d(%d,%d)\t", p, room.players_map[p].playerInfo.roleId, room.players_map[p].playerInfo.teamId)
					}
					fmt.Println()
					fmt.Println()
				}
				fmt.Println("-------------------------------------------")
			}

			// switch frame
			cur_game_frame := pb.GameFrame{}
			cur_frame_id_real := uint32((b_time - room.game_start_time) / room.game_frame_time_per_frame)
			cur_frame_id := room.game_frame_cur_frame_id
			if cur_frame_id_real-cur_frame_id >= 10 {
				//fmt.Printf("frame delay. [frame_id_real = %d, frame_id_counter = %d, room_id = %d]\n", cur_frame_id_real, cur_frame_id, room.roomId)
			}

			//if room.game_command_total > 0 || b_time-room.game_last_sync_frame_time > cfg_frame_timeout_void_command {
			cur_game_frame.FrameId = cur_frame_id
			//cur_game_frame.FrameIdFrom = room.game_frame_last_sync_frame_id
			cur_game_frame.Commands = room.game_command[0:room.game_command_total]
			frame_buf, _ := proto.Marshal(&cur_game_frame)
			if len(frame_buf) > int(pb.Constant_Message_MaxBody_Len) {
				glog.Infof("game frame buffer overflow. [max_command_buffer = %d, room = %d]\n", pb.Constant_Message_MaxBody_Len, room.roomId)
			} else {
				//fmt.Printf("switch frame. [cur_frame_id = %d, room = %d, buff_len = %d, buf = %v]\n", cur_frame_id, room.roomId, len(frame_buf), frame_buf)
				BroadcastMessage(&MsgCommon{reply_message_id: pb.CS_MessageId_S2C_Sync_Frame, reply_buf: frame_buf}, &room.players, 0, true)

				// 写入帧数据到文件
				//room.game_all_frame.PushBack(&cur_game_frame)
				if cfg_room_record_flag {
					go func() {
						if _, err := room.game_frame_file.Write(frame_buf); err != nil {
							fmt.Printf("fail to write file. [file = %s]\n", room.game_frame_file_name)
						}
					}()
				}
			}

			room.game_last_sync_frame_time = b_time
			room.game_frame_last_sync_frame_id = cur_frame_id
			//}
			room.game_command_total = 0
		} else if room.state == RoomState_Preparing {
			// 检查房间游戏开启超时
			if b_time-create_time >= int64(time.Second)*300 {
				// 解散房间
				room.ch <- &RoomAction{action: RoomAction_RoomCancel}
			}
		}

		if room.state == RoomState_NonNormal_GameOver || room.state == RoomState_GameOver || bStopServer {
			room.ch <- nil
			room.Free()
			return
		}
	}
}
