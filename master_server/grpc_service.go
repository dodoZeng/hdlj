package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
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
	"google.golang.org/grpc/reflection"
	"roisoft.com/hdlj/common"
	//"roisoft.com/hdlj/common/pool"
	pb "roisoft.com/hdlj/proto"
)

type grpc_service struct {
	wg   *sync.WaitGroup
	s    *grpc.Server
	stop bool
}

type agent_cluster struct {
	nodes      []common.NodeInfo
	node_total uint32
	index      uint32
	id2index   sync.Map
}

type game_cluster struct {
	nodes      []common.NodeInfo
	node_total uint32
	id2index   sync.Map
}

const (
	redis_key_session_format = "hdlj.master.sessions.%d"

	agent_cluster_node_max = 1024
	game_cluster_node_max  = 1024
)

var (
	//poolRedisClients pool.Pool
	chRedisClients chan *redis.Client
	agentCluster   agent_cluster
	gameCluster    game_cluster
	cur_player_id  uint64
)

func init() {
	agentCluster.nodes = make([]common.NodeInfo, agent_cluster_node_max)
	gameCluster.nodes = make([]common.NodeInfo, game_cluster_node_max)
}

func init_redis_pool() (bool, string) {
	/*
		factory := func() (interface{}, error) {
			return redis.NewClient(&redis.Options{
				Addr:        cfg_server_addr_redis,
				Password:    cfg_server_pwd_redis,
				DB:          0, // use default DB
				MaxRetries:  3,
				DialTimeout: time.Second * 5,
			}), nil
		}
		iterater_fun := func(client interface{}) (bool, string) {
			c := client.(*redis.Client)
			chRedisClients <- c

			if _, err := c.Ping().Result(); err != nil {
				return false, err.Error()
			}
			return true, ""
		}

		pool_size := cfg_redis_client_size_per_cpu * runtime.NumCPU()
		if p, err := pool.NewChannelPool(pool_size, pool_size, factory); err != nil {
			return false, err.Error()
		} else {
			poolRedisClients = p
			chRedisClients = make(chan *redis.Client, pool_size)
		}
		return poolRedisClients.Iterate(iterater_fun)
	*/

	cfg_client_pool_size = cfg_redis_client_size_per_cpu * runtime.NumCPU()
	chRedisClients = make(chan *redis.Client, cfg_client_pool_size)
	for i := 0; i < cfg_client_pool_size; i = i + 1 {
		c := redis.NewClient(&redis.Options{
			Addr:        cfg_server_addr_redis,
			Password:    cfg_server_pwd_redis,
			DB:          0, // use default DB
			MaxRetries:  3,
			DialTimeout: time.Second * 5,
		})
		if _, err := c.Ping().Result(); err != nil {
			return false, err.Error()
		} else {
			chRedisClients <- c
		}

	}
	return true, ""
}

func load_cur_player_id() (bool, string) {
	if cfg_node_id != common.NodeId_Roisoft_Master {
		return true, ""
	}

	if !common.CheckFileIsExist(cfg_player_id_config) {
		return false, fmt.Sprintf("%s is not exist.", cfg_player_id_config)
	}
	f, err := os.OpenFile(cfg_player_id_config, os.O_RDONLY|os.O_EXCL, 0666)
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()

	var buf [128]byte
	var ids string
	if n, err := f.Read(buf[0:]); err != nil && err != io.EOF {
		return false, err.Error()
	} else if n == 0 {
		return false, fmt.Sprintf("%s is empty.", cfg_player_id_config)
	} else {
		ids = string(buf[0:n])
	}
	if val, err := strconv.Atoi(ids); err != nil {
		return false, fmt.Sprintf("%s is not a integer. [content = %s]", cfg_player_id_config, ids)
	} else {
		cur_player_id = uint64(val)
	}
	fmt.Println("current player id = ", cur_player_id)

	return true, ""
}

func store_cur_player_id() (bool, string) {
	if cfg_node_id != common.NodeId_Roisoft_Master {
		return true, ""
	}

	// 把ID保存到文件中
	f, err := os.OpenFile(cfg_player_id_config, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()

	if _, err := io.WriteString(f, fmt.Sprintf("%d", cur_player_id)); err != nil {
		return false, err.Error()
	}

	return true, ""
}

func new_session_id() string {
	return fmt.Sprintf("%s.%s", common.Rand().Hex(), cfg_node_id_suffix)
}

func NewGrpcService() *grpc_service {
	return &grpc_service{
		wg:   &sync.WaitGroup{},
		stop: true,
	}
}

func (service *grpc_service) start_grpc_service(addr string) (bool, string) {
	service.s = grpc.NewServer()

	// load player id
	if ok, err := load_cur_player_id(); !ok {
		return false, err
	}
	// init redis pool
	if ok, err := init_redis_pool(); !ok {
		return false, err
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		glog.Errorf("failed to listen: %v\n", err)
		return false, err.Error()
	}

	pb.RegisterMasterServiceServer(service.s, service)
	reflection.Register(service.s)
	go func() {
		defer glog.Infoln("routine exit: grpc.")
		defer fmt.Println("routine exit: grpc.")

		if err := service.s.Serve(lis); err != nil {
			glog.Errorf("failed to grpc serve: %v\n", err)
		}
	}()
	go service.taskRoutine()
	service.stop = false
	return true, ""
}

func (service *grpc_service) stop_grpc_service() {
	service.s.Stop()
	service.stop = true

	i := 0
	for {
		if i >= cfg_client_pool_size {
			break
		}

		select {
		case c := <-chRedisClients:
			c.Close()
			i++
		default:
		}
	}

	service.wg.Wait()
}

func (s *grpc_service) taskRoutine() {
	s.wg.Add(1)
	defer s.wg.Done()

	ticker := time.NewTicker(time.Duration(cfg_get_config_interval))
	defer ticker.Stop()

	for range ticker.C {

		if cfg_node_id == common.NodeId_Roisoft_Master {
			// save player id
			if ok, err := store_cur_player_id(); !ok {
				glog.Errorf("fail to save player id. [err = %s]\n", err)
			}
		} else {
			// get config from consul
			if ok, err := get_config_from_consul(); !ok {
				glog.Errorf("fail to get config from consul. [err = %s]\n", err)
			}
		}

		if s.stop {
			break
		}
	}
}

func (s *grpc_service) GetNewPlayerId(ctx context.Context, in *pb.VoidRequest) (*pb.KeyPlayerId, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	reply := pb.KeyPlayerId{Key: 0}

	if cfg_node_id != common.NodeId_Roisoft_Master {
		return &reply, errors.New("not leader master.")
	}

	// todo
	reply.Key = atomic.AddUint64(&cur_player_id, 1)

	return &reply, nil
}

func (s *grpc_service) Signin(ctx context.Context, in *pb.SessionRequest) (*pb.SessionReply, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	reply := pb.SessionReply{Result: &pb.ErrorInfo{Code: int32(pb.ErrorCode_CS_INVALID_PARAMETER)}}

	if in.PlayerId != 0 {
		redisClient := <-chRedisClients
		defer func() {
			chRedisClients <- redisClient
		}()

		// check connection
		if err := redisClient.Ping().Err(); err != nil {
			is_ok := false
			new_c := redis.NewClient(&redis.Options{
				Addr:        cfg_server_addr_redis,
				Password:    cfg_server_pwd_redis,
				DB:          0, // use default DB
				MaxRetries:  3,
				DialTimeout: time.Second * 5,
			})
			if err := new_c.Ping().Err(); err == nil {
				redisClient = new_c
				is_ok = true
			}
			if !is_ok {
				reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
				reply.Result.Context = fmt.Sprintf("redis error. [err = %v]", err)
				return &reply, nil
			}
		}

		// todo
		var sessionInfo pb.RSessionInfo

		key := fmt.Sprintf(redis_key_session_format, in.PlayerId)
		can_reallocate_agent := true

		// check sessionid if exist
		if buf, err := redisClient.Get(key).Bytes(); err != nil {
			proto.Unmarshal(buf, &sessionInfo)
			if sessionInfo.Online {
				if v, ok := agentCluster.id2index.Load(sessionInfo.LastAgentId); ok && agentCluster.nodes[v.(uint)].Available {
					can_reallocate_agent = false
				}
			}
		}
		sessionInfo.SessionId = new_session_id()
		sessionInfo.Expiration = time.Now().AddDate(0, 0, cfg_session_expiration_in_hour).UnixNano()

		if can_reallocate_agent && (in.ReallocateAgent || sessionInfo.LastAgentId == "") {
			agent_id := ""
			agent_addr := ""

			// allocate for a player
			for i := uint32(0); i < agentCluster.node_total; i++ {
				if agentCluster.nodes[agentCluster.index%agentCluster.node_total].Available {
					agent_id = agentCluster.nodes[agentCluster.index%agentCluster.node_total].Id
					agent_addr = agentCluster.nodes[agentCluster.index%agentCluster.node_total].Addr
					atomic.AddUint32(&agentCluster.index, 1)
					break
				}
				atomic.AddUint32(&agentCluster.index, 1)
			}

			sessionInfo.LastAgentId = agent_id
			sessionInfo.LastAgentAddr = agent_addr
		}

		if sessionInfo.LastAgentId == "" {
			reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
			reply.Result.Context = "not exist available agent."
		} else {
			buf, _ := proto.Marshal(&sessionInfo)
			if err := redisClient.Set(key, buf, time.Duration(sessionInfo.Expiration)).Err(); err != nil {
				reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
				reply.Result.Context = fmt.Sprintf("save session to redis error. [err = %v]", err)
			} else {
				//fmt.Printf("save key to redis. [key = %s, val = %v]\n", key, buf)

				reply.Result.Code = int32(pb.ErrorCode_CS_OK)
				reply.SessionId = sessionInfo.SessionId
				reply.AgentId = sessionInfo.LastAgentId
				reply.AgentAddr = sessionInfo.LastAgentAddr
			}
		}
	}

	return &reply, nil
}

func (s *grpc_service) CheckSession(ctx context.Context, in *pb.SessionRequest) (*pb.SessionReply, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	reply := pb.SessionReply{Result: &pb.ErrorInfo{Code: int32(pb.ErrorCode_CS_INVALID_PARAMETER)}}

	if in.PlayerId != 0 && in.GetSessionId() != "" {
		redisClient := <-chRedisClients
		defer func() {
			chRedisClients <- redisClient
		}()

		// check connection
		if err := redisClient.Ping().Err(); err != nil {
			is_ok := false
			new_c := redis.NewClient(&redis.Options{
				Addr:        cfg_server_addr_redis,
				Password:    cfg_server_pwd_redis,
				DB:          0, // use default DB
				MaxRetries:  3,
				DialTimeout: time.Second * 5,
			})
			if err := new_c.Ping().Err(); err == nil {
				redisClient = new_c
				is_ok = true
			}
			if !is_ok {
				reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
				reply.Result.Context = fmt.Sprintf("redis error. [err = %v]", err)
				return &reply, nil
			}
		}

		// todo
		key := fmt.Sprintf(redis_key_session_format, in.PlayerId)
		if buf, err := redisClient.Get(key).Bytes(); err != nil {
			reply.Result.Code = int32(pb.ErrorCode_CS_INVALID_SESSION)
			reply.Result.Context = fmt.Sprintf("session not found. [err = %v]", err)
		} else {
			var sessionInfo pb.RSessionInfo

			if err := proto.Unmarshal(buf, &sessionInfo); err == nil {
				if sessionInfo.SessionId != in.SessionId || time.Now().UnixNano()-sessionInfo.Expiration >= 0 {
					reply.Result.Code = int32(pb.ErrorCode_CS_INVALID_SESSION)
					reply.Result.Context = fmt.Sprintf("session over the expiration")
				} else {
					need_save := false

					if in.ReallocateAgent {
						agent_id := ""
						agent_addr := ""

						// allocate for a player
						for i := uint32(0); i < agentCluster.node_total; i++ {
							if agentCluster.nodes[agentCluster.index%agentCluster.node_total].Available {
								agent_id = agentCluster.nodes[agentCluster.index%agentCluster.node_total].Id
								agent_addr = agentCluster.nodes[agentCluster.index%agentCluster.node_total].Addr
								atomic.AddUint32(&agentCluster.index, 1)
								break
							}
							atomic.AddUint32(&agentCluster.index, 1)
						}

						if agent_addr == "" {
							reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
							reply.Result.Context = "not exist available agent."
						} else {
							reply.Result.Code = int32(pb.ErrorCode_CS_OK)
							reply.AgentId = agent_id
							reply.AgentAddr = agent_addr

							sessionInfo.LastAgentId = agent_id
							sessionInfo.LastAgentAddr = agent_addr
							need_save = true
						}
					} else {
						reply.Result.Code = int32(pb.ErrorCode_CS_OK)
						reply.AgentId = sessionInfo.LastAgentId
						reply.AgentAddr = sessionInfo.LastAgentAddr
					}

					//  避免重复登录：应该检查当前玩家是否在线，若在线，则返回错误
					if in.GetGameServerId() != "" {
						if sessionInfo.Online && sessionInfo.LastGameServerId != in.GameServerId {
							reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
							reply.Result.Context = "the session is signin the other gameserver."
						} else {
							reply.LastGameServerId = sessionInfo.LastGameServerId
							sessionInfo.LastGameServerId = in.GameServerId
							sessionInfo.Online = true
							need_save = true
						}
					}

					// save session into redis
					if need_save {
						buf, _ := proto.Marshal(&sessionInfo)
						if err := redisClient.Set(key, buf, time.Hour*time.Duration(cfg_session_expiration_in_hour)).Err(); err != nil {
							reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
							reply.Result.Context = fmt.Sprintf("save session to redis error. [err = %v]", err)
							glog.Errorf("save session to redis error. [err = %v]", err)
						}
					}
				}
			}
		}
	}

	return &reply, nil
}

func (s *grpc_service) Offline(ctx context.Context, in *pb.SessionRequest) (*pb.VoidReply, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	reply := pb.VoidReply{}

	if in.PlayerId != 0 { // && in.GetSessionId() != "" {
		redisClient := <-chRedisClients
		defer func() {
			chRedisClients <- redisClient
		}()

		// check connection
		if err := redisClient.Ping().Err(); err != nil {
			is_ok := false
			new_c := redis.NewClient(&redis.Options{
				Addr:        cfg_server_addr_redis,
				Password:    cfg_server_pwd_redis,
				DB:          0, // use default DB
				MaxRetries:  3,
				DialTimeout: time.Second * 5,
			})
			if err := new_c.Ping().Err(); err == nil {
				redisClient = new_c
				is_ok = true
			}
			if !is_ok {
				return nil, errors.New(fmt.Sprintf("grpc(Offline) error: %v", err))
			}
		}

		// todo
		key := fmt.Sprintf(redis_key_session_format, in.PlayerId)
		if buf, err := redisClient.Get(key).Bytes(); err == nil {
			var sessionInfo pb.RSessionInfo
			if err := proto.Unmarshal(buf, &sessionInfo); err == nil { // && sessionInfo.SessionId == in.SessionId {
				// save session into redis
				sessionInfo.Online = false
				buf, _ := proto.Marshal(&sessionInfo)
				if err := redisClient.Set(key, buf, time.Hour*time.Duration(cfg_session_expiration_in_hour)).Err(); err != nil {
					glog.Errorf("save session to redis error. [err = %v]", err)
					return nil, errors.New(fmt.Sprintf("grpc(Offline) error: %v", err))
				} else {
					return &reply, nil
				}
			}
		}
	}

	return nil, errors.New(fmt.Sprintf("grpc(Offline) invalid parameter."))
}

func (s *grpc_service) SyncLoadInfo(ctx context.Context, in *pb.LoadInfo) (*pb.VoidReply, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	if in.GetNodeId() != "" {
		if index, ok := gameCluster.id2index.Load(in.GetNodeId()); ok {
			i := index.(uint)
			gameCluster.nodes[i].Load = int(in.GetLoad())
		}
	}

	return &pb.VoidReply{}, nil
}

func (s *grpc_service) GetGameServerList(ctx context.Context, in *pb.SessionInfo) (*pb.GameServerList, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	reply := pb.GameServerList{}

	if in.PlayerId != 0 && len(in.GetSessionId()) > 0 {
		redisClient := <-chRedisClients
		defer func() {
			chRedisClients <- redisClient
		}()

		// check connection
		if err := redisClient.Ping().Err(); err != nil {
			is_ok := false
			new_c := redis.NewClient(&redis.Options{
				Addr:        cfg_server_addr_redis,
				Password:    cfg_server_pwd_redis,
				DB:          0, // use default DB
				MaxRetries:  3,
				DialTimeout: time.Second * 5,
			})
			if err := new_c.Ping().Err(); err == nil {
				redisClient = new_c
				is_ok = true
			}
			if !is_ok {
				return &reply, nil
			}
		}

		key := fmt.Sprintf(redis_key_session_format, in.PlayerId)
		if buf, e := redisClient.Get(key).Bytes(); e == nil {
			var sessionInfo pb.RSessionInfo

			// check session
			if err := proto.Unmarshal(buf, &sessionInfo); err == nil {
				if sessionInfo.SessionId == in.SessionId && time.Now().UnixNano()-sessionInfo.Expiration < 0 {
					for i := uint32(0); i < gameCluster.node_total; i++ {
						if gameCluster.nodes[i].Available {
							reply.Servers = append(reply.Servers, &pb.GameServerInfo{NodeId: gameCluster.nodes[i].Id, NodeName: gameCluster.nodes[i].Name, NodePort: uint32(gameCluster.nodes[i].Port), TotalPlayer: int32(gameCluster.nodes[i].Load)})
						}
					}
					fmt.Println("game list reply: ", reply)
				}
			}
		} else {
			fmt.Printf("key not exist. [key = %s]\n", key)
		}
	}

	return &reply, nil
}
