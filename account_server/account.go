package main

import (
	"fmt"
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

const (
	redis_key_signin_total_format = "hdlj.account.signin_total.%s"
)

var (
	dbCluster     common.DbCluster
	masterCluster common.MasterCluster
	redisCluster  common.RedisCluster
	chFreeClient  chan bool
)

func conn_to_all_server() (bool, string) {
	chFreeClient = make(chan bool, cfg_client_pool_size)
	for i := 0; i < cfg_client_pool_size; i++ {
		chFreeClient <- true
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

	// 连接所有master和leaderMaster
	if conn, err := grpc.Dial(masterCluster.LeaderAddr, grpc.WithInsecure()); err != nil {
		return false, err.Error()
	} else {
		c := pb.NewMasterServiceClient(conn)
		masterCluster.LeaderConn = conn
		masterCluster.LeaderClient = c
	}
	for i, v := range masterCluster.Nodes {
		if conn, err := grpc.Dial(v.Addr, grpc.WithInsecure()); err != nil {
			return false, err.Error()
		} else {
			c := pb.NewMasterServiceClient(conn)
			masterCluster.Conns[i] = conn
			masterCluster.Clients[i] = c
		}
	}

	// 连接所有redis
	for i, v := range redisCluster.Nodes {
		c := redis.NewClient(&redis.Options{
			Addr:        v.Addr,
			Password:    "",
			DB:          0, // use default DB
			MaxRetries:  3,
			DialTimeout: time.Second * 5,
		})
		if _, err := c.Ping().Result(); err != nil {
			return false, err.Error()
		} else {
			redisCluster.Conns[i] = c
		}
	}

	return true, ""
}

func signup(in *pb.SignupRequest) *pb.ErrorInfo {

	reply := pb.ErrorInfo{Code: int32(pb.ErrorCode_CS_INVALID_PARAMETER), Context: "invalid parameter."}

	for i := 0; i < 1; i++ {
		if len(in.GetAccount()) == 0 || len(in.GetPassword()) == 0 || len(in.GetAttachCode()) == 0 {
			fmt.Printf("signup: invalid parameter. param = %+v\n", *in)
		} else {
			<-chFreeClient
			defer func() {
				chFreeClient <- true
			}()

			redis := redisCluster.GetNodeByString(in.AttachCode)
			if redis != nil {
				// check attach code
				if cfg_need_token {
					key := fmt.Sprintf(common.Redis_Key_Captcha_Format, in.AttachCode)
					if _, err := redis.Get(key).Result(); err != nil {
						reply.Code = int32(pb.ErrorCode_CS_INVALID_ATTACHCODE)
						reply.Context = "invalid attach code"
						break
					}
					redis.Del(key)
				}

				// create account
				if _, code, err := checkAndMakeAccount(in.GetAccount(), in.GetPassword(), masterCluster.GetLeader()); code != int32(pb.ErrorCode_CS_OK) {
					reply.Code = code
					reply.Context = err
				} else {
					reply.Code = int32(pb.ErrorCode_CS_OK)
					reply.Context = ""
				}
			} else {
				reply.Code = int32(pb.ErrorCode_CS_UNKNOW)
				reply.Context = "no redis client"
			}
		}
	}

	return &reply
}

func signin(in *pb.SigninRequest) *pb.SigninReply {

	reply := pb.SigninReply{Result: &pb.ErrorInfo{Code: int32(pb.ErrorCode_CS_INVALID_PARAMETER), Context: "invalid parameter."}}

	for i := 0; i < 1; i++ {
		if (int32(pb.AccountType_Session) == in.AccountType && (len(in.GetSessionId()) == 0 || in.PlayerId == 0)) ||
			(int32(pb.AccountType_Session) != in.AccountType && (len(in.GetAccount()) == 0 || (int32(pb.AccountType_Guest) != in.AccountType && len(in.GetPassword()) == 0))) {
			fmt.Printf("signin: invalid parameter. param = %+v\n", *in)
		} else {
			<-chFreeClient
			defer func() {
				chFreeClient <- true
			}()

			// session signin
			if int32(pb.AccountType_Session) == in.AccountType {
				ctx := context.Background()
				if cfg_grpc_client_timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
					defer cancel()
				}
				request := &pb.SessionRequest{PlayerId: in.PlayerId, SessionId: in.SessionId, ReallocateAgent: in.ReallocateAgent}
				master := masterCluster.GetNodeByPlayerId(in.PlayerId)

				if master != nil {
					if r, err := master.CheckSession(ctx, request); err != nil {
						reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
						reply.Result.Context = err.Error()
					} else {
						reply.Result.Code = r.Result.Code
						reply.Result.Context = r.Result.Context
						if r.Result.Code == int32(pb.ErrorCode_CS_OK) {
							reply.AgentId = r.AgentId
							reply.AgentAddr = r.AgentAddr
						}
					}
				}
			} else {
				need_check_attch_code := false

				if int32(pb.AccountType_Guest) == in.AccountType {
					if in.PlayerId == 0 {
						need_check_attch_code = true
					}
				} else {
					// 根据密码错误登录次数，决定是否需要校验attach_code
					key := fmt.Sprintf(redis_key_signin_total_format, in.Account)
					redis := redisCluster.GetNodeByString(in.Account)

					if redis != nil {
						if val, err := redis.Get(key).Int64(); err == nil && val >= int64(cfg_pwd_input_total) {
							need_check_attch_code = true
						}
					} else {
						reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
						reply.Result.Context = "no redis client"
					}
				}

				// check attach code
				if cfg_need_token && need_check_attch_code {
					redis := redisCluster.GetNodeByString(in.AttachCode)
					key := fmt.Sprintf(common.Redis_Key_Captcha_Format, in.AttachCode)

					if redis == nil {
						reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
						reply.Result.Context = "no redis client"
						break
					}
					if _, err := redis.Get(key).Result(); err != nil {
						reply.Result.Code = int32(pb.ErrorCode_CS_INVALID_ATTACHCODE)
						reply.Result.Context = "invalid attach code"
						break
					}
					redis.Del(key)
				}

				db_account, ok, err := getAccountDataByAccount(in.Account)
				if !ok {
					fmt.Println("invalid account. account = ", in.Account)
					reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
					reply.Result.Context = err
					break
				}

				// check account
				if db_account == nil {
					if int32(pb.AccountType_Guest) == in.AccountType && in.PlayerId == 0 {
						// if guest not exist, create it
						accountInfo := &pb.SignupRequest{Account: in.Account, Password: in.Password}
						if _, code, err := checkAndMakeAccount(accountInfo.GetAccount(), accountInfo.GetPassword(), masterCluster.GetLeader()); code == int32(pb.ErrorCode_CS_OK) {
							if acc, ok, err := getAccountDataByAccount(in.Account); !ok || acc == nil {
								reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
								reply.Result.Context = err
								break
							} else {
								db_account = acc
							}
						} else {
							reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
							reply.Result.Context = err
							break
						}
					} else {
						reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
						reply.Result.Context = err
						break
					}
				}
				if int32(pb.AccountType_Guest) == in.AccountType {
					// check player id
					if in.PlayerId != 0 && in.PlayerId != db_account.PlayerId {
						reply.Result.Code = int32(pb.ErrorCode_CS_INVALID_PLAYERID)
						reply.Result.Context = "invalid player id"
						break
					}
				} else {
					redis := redisCluster.GetNodeByString(in.Account)
					key := fmt.Sprintf(redis_key_signin_total_format, in.Account)

					// check password
					if in.Password != db_account.Password {
						reply.Result.Code = int32(pb.ErrorCode_CS_INVALID_PASSWORD)
						reply.Result.Context = "invalid password"

						if redis != nil {
							n := int64(1)

							if v, err := redis.Get(key).Int64(); err == nil {
								n = v + 1
								//fmt.Printf("Get. [key = %s, signin_total = %d]\n", key, n)
							}
							redis.Set(key, n, time.Minute*30)
						} else {
							reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
							reply.Result.Context = "no redis"
						}
						break
					} else {
						if redis != nil {
							//fmt.Printf("Del. [key = %s]\n", key)
							redis.Del(fmt.Sprintf(redis_key_signin_total_format, in.Account))
						}
					}
				}

				// allocate sessionid & agent
				ctx := context.Background()
				if cfg_grpc_client_timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
					defer cancel()
				}
				request := &pb.SessionRequest{PlayerId: db_account.PlayerId, ReallocateAgent: in.ReallocateAgent}
				master := masterCluster.GetNodeByPlayerId(db_account.PlayerId)

				if master == nil {
					reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
				} else if r, err := master.Signin(ctx, request); err != nil {
					reply.Result.Code = int32(pb.ErrorCode_CS_UNKNOW)
					reply.Result.Context = err.Error()
				} else {
					reply.Result.Code = r.Result.Code
					reply.Result.Context = r.Result.Context
					if r.Result.Code == int32(pb.ErrorCode_CS_OK) {
						reply.PlayerId = db_account.PlayerId
						reply.SessionId = r.SessionId
						reply.AgentId = r.AgentId
						reply.AgentAddr = r.AgentAddr
					}
				}
			}
		}
	}

	return &reply
}

func getGameServerList(in *pb.SessionInfo) *pb.GameServerList {

	reply := pb.GameServerList{}

	if in.PlayerId != 0 && len(in.GetSessionId()) > 0 {
		ctx := context.Background()
		if cfg_grpc_client_timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
			defer cancel()
		}
		request := &pb.SessionInfo{PlayerId: in.PlayerId, SessionId: in.SessionId}
		master := masterCluster.GetNodeByPlayerId(in.PlayerId)
		if master != nil {
			if r, err := master.GetGameServerList(ctx, request); err == nil {
				reply.Servers = r.Servers
			}
		} else {
			fmt.Printf("master server not exist. [playerid = %d]\n", in.PlayerId)
		}
	} else {
		fmt.Println("invalid parameter.")
	}

	return &reply
}

func getAccountData(in *pb.SessionInfo) *pb.AccountData {

	reply := &pb.AccountData{}

	if in.PlayerId != 0 && len(in.GetSessionId()) > 0 {
		if ok, err := checkSession(in); ok {
			ctx := context.Background()
			if cfg_grpc_client_timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
				defer cancel()
			}
			key := pb.KeyPlayerId{Key: in.PlayerId}
			key_buf, _ := proto.Marshal(&key)
			request := &pb.DbRequest{Type: uint32(pb.OpType_Account), Method: uint32(pb.OpMethod_GET), Key: key_buf}

			if db := dbCluster.GetNodeByPlayerId(in.PlayerId); db != nil {
				if r, err := db.DbOperation(ctx, request); err == nil && r.GetValue() != nil {
					// parse account data
					data := pb.DbAccountData{}
					if err := proto.Unmarshal(r.Value, &data); err == nil {
						reply = data.Account
					}
				}
			}
		} else {
			fmt.Println(err)
		}
	}

	return reply
}

func checkIDCard(in *pb.CheckIdCardRequest) *pb.ErrorInfo {

	reply := pb.ErrorInfo{Code: int32(pb.ErrorCode_CS_INVALID_PARAMETER), Context: "invalid parameter."}

	for i := 0; i < 1; i++ {
		if in.GetSession() == nil || in.GetIdCard() == nil ||
			in.Session.PlayerId == 0 || len(in.Session.GetSessionId()) <= 0 ||
			len(in.IdCard.GetId()) != 18 || len(in.IdCard.GetName()) <= 0 {
			fmt.Println("invalid parameter.")
		} else {
			// 检查Session合法性
			if ok, err := checkSession(in.GetSession()); ok {

				// 检查身份证合法性
				// todo:接入公安局的身份证验证系统

				// 保存身份证信息
				if data, _, err := getDbAccountData(in.Session.PlayerId); data != nil {
					data.Account.IdNumber = in.IdCard.Id
					data.Account.IdName = in.IdCard.Name

					if ok, err := setDbAccountData(data); ok {
						reply.Code = int32(pb.ErrorCode_CS_OK)
						reply.Context = ""
					} else {
						reply.Context = err
					}
				} else {
					reply.Context = err
				}

			} else {
				reply.Context = err
			}
		}
	}

	return &reply
}

func checkSession(s *pb.SessionInfo) (bool, string) {

	if s == nil || s.PlayerId == 0 || len(s.GetSessionId()) <= 0 {
		return false, "invalid parameter."
	}

	ctx := context.Background()
	if cfg_grpc_client_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
		defer cancel()
	}
	request := &pb.SessionRequest{PlayerId: s.PlayerId, SessionId: s.SessionId}
	master := masterCluster.GetNodeByPlayerId(s.PlayerId)

	if master != nil {
		if r, err := master.CheckSession(ctx, request); err == nil && r.Result.Code == int32(pb.ErrorCode_CS_OK) {
			return true, ""
		} else {
			return false, fmt.Sprintf("fail to check session. [session = %v, err = %v]", s, err)
		}
	} else {
		return false, "not exist master."
	}
}

func getAccountDataByAccount(account string) (*pb.DbAccountData, bool, string) {
	db := dbCluster.GetNodeByString(account)
	if db == nil {
		return nil, false, "db is not available."
	}

	ctx := context.Background()
	if cfg_grpc_client_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
		defer cancel()
	}
	buf, _ := proto.Marshal(&pb.KeyString{Key: account})
	request := &pb.DbRequest{Type: uint32(pb.OpType_Account_Index), Method: uint32(pb.OpMethod_GET), Key: buf}

	if r, err := db.DbOperation(ctx, request); err != nil {
		return nil, false, err.Error()
	} else {
		if r.GetValue() == nil {
			return nil, true, ""
		}

		// parse player id
		key_playerid := pb.KeyPlayerId{}
		if err := proto.Unmarshal(r.Value, &key_playerid); err != nil {
			return nil, false, "fail to parse player id"
		} else {
			return getDbAccountData(key_playerid.Key)
		}
	}
}

func getDbAccountData(playerid uint64) (*pb.DbAccountData, bool, string) {
	key_playerid := pb.KeyPlayerId{Key: playerid}
	key_buf, _ := proto.Marshal(&key_playerid)

	db := dbCluster.GetNodeByPlayerId(playerid)
	if db == nil {
		return nil, false, "db is not available."
	}

	ctx := context.Background()
	if cfg_grpc_client_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
		defer cancel()
	}
	request := &pb.DbRequest{Type: uint32(pb.OpType_Account), Method: uint32(pb.OpMethod_GET), Key: key_buf}

	if r, err := db.DbOperation(ctx, request); err != nil {
		return nil, false, err.Error()
	} else {
		if r.GetValue() == nil {
			return nil, false, fmt.Sprintf("data is not exist. [playerid = %d]", playerid)
		}

		// parse account data
		data := pb.DbAccountData{}
		if err := proto.Unmarshal(r.Value, &data); err != nil {
			return nil, false, "fail to parse account data"
		} else {
			return &data, true, ""
		}
	}
}

func setDbAccountData(data *pb.DbAccountData) (bool, string) {
	db := dbCluster.GetNodeByPlayerId(data.PlayerId)
	if db == nil {
		return false, "db is not available."
	}

	playerid_buf, _ := proto.Marshal(&pb.KeyPlayerId{Key: data.PlayerId})
	data_buf, _ := proto.Marshal(data)
	dbrequest := &pb.DbRequest{Type: uint32(pb.OpType_Account), Method: uint32(pb.OpMethod_PUT), Key: playerid_buf, Value: data_buf}
	ctx := context.Background()
	if cfg_grpc_client_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
		defer cancel()
	}

	if _, err := db.DbOperation(ctx, dbrequest); err != nil {
		return false, err.Error()
	}

	return true, ""
}

func checkAndMakeAccount(param_acc string, param_pwd string, master pb.MasterServiceClient) (uint64, int32, string) {
	if master == nil {
		return 0, int32(pb.ErrorCode_CS_UNKNOW), "master is nil."
	}

	db := dbCluster.GetNodeByString(param_acc)
	if db == nil {
		return 0, int32(pb.ErrorCode_CS_UNKNOW), "db is not available."
	}

	// check account if exist
	ctx := context.Background()
	if cfg_grpc_client_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
		defer cancel()
	}
	key_buf, _ := proto.Marshal(&pb.KeyString{Key: param_acc})
	dbrequest := &pb.DbRequest{Type: uint32(pb.OpType_Account_Index), Method: uint32(pb.OpMethod_CHK), Key: key_buf}

	if r, err := db.DbOperation(ctx, dbrequest); err != nil {
		return 0, int32(pb.ErrorCode_CS_UNKNOW), err.Error()
	} else {
		if r.Value[0] > 0 {
			return 0, int32(pb.ErrorCode_CS_ACCOUNT_EXIST), "account is exist."
		}
	}

	// get a new player id and save account
	if cfg_grpc_client_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
		defer cancel()
	}
	if new_player_id, err := master.GetNewPlayerId(ctx, &pb.VoidRequest{}); err != nil {
		return 0, int32(pb.ErrorCode_CS_UNKNOW), err.Error()
	} else {
		// 	save account index
		playerid_buf, _ := proto.Marshal(new_player_id)
		dbrequest = &pb.DbRequest{Type: uint32(pb.OpType_Account_Index), Method: uint32(pb.OpMethod_PUT), Key: key_buf, Value: playerid_buf}
		if cfg_grpc_client_timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg_grpc_client_timeout))
			defer cancel()
		}
		if _, err := db.DbOperation(ctx, dbrequest); err != nil {
			glog.Errorf("fail to save account index [node_id = %s, err = %s]\n", cfg_node_id, err.Error())
			return 0, int32(pb.ErrorCode_CS_UNKNOW), err.Error()
		}

		// save account data
		db = dbCluster.GetNodeByPlayerId(new_player_id.Key)
		if db == nil {
			return 0, int32(pb.ErrorCode_CS_UNKNOW), "db is not available."
		}

		var data pb.DbAccountData
		data.Account = &pb.AccountData{Account: param_acc}
		data.PlayerId = new_player_id.Key
		data.Password = param_pwd
		data.SignupTime = time.Now().UnixNano()

		if ok, err := setDbAccountData(&data); !ok {
			return 0, int32(pb.ErrorCode_CS_UNKNOW), err
		}

		return new_player_id.Key, int32(pb.ErrorCode_CS_OK), ""
	}
}
