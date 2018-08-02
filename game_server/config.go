package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)
import (
	consulapi "github.com/hashicorp/consul/api"
	"google.golang.org/grpc"
	"roisoft.com/hdlj/common"
	"roisoft.com/hdlj/common/config"
	pb "roisoft.com/hdlj/proto"
)

const (
	cfg_frame_max_command_num = 256 // 帧同步：每帧最大指令数

	db_proxy_tag = "hdlj.db.cluster"
	master_tag   = "hdlj.master.cluster"
)

var (
	cfg_file = "./game_server.cfg"

	cfg_node_addr                     = "0.0.0.0"
	cfg_node_port                     = "6000"
	cfg_node_id                       = "hdlj.game.test1"
	cfg_node_name                     = "test1"
	cfg_node_tags                     = []string{"hdlj.game.cluster"}
	cfg_node_check_port               = "9000"
	cfg_node_check_url                = "/check"
	cfg_node_check_timeout            = "5s"
	cfg_node_check_interval           = "10s"
	cfg_node_check_deregister_timeout = "30s"

	cfg_io_timeout                = int64(100)
	cfg_io_pool_size              = int(1e5)
	cfg_unauth_io_coroutine_total = 1

	cfg_server_addr_consul_agent = "0.0.0.0:8500"
	cfg_server_addr_redis        = "0.0.0.0:6379"
	cfg_server_pwd_redis         = ""

	cfg_log_custom_level = "1"
	cfg_log_dir          = "./log"

	cfg_session_timeout_heartbeat = int64(7e9) // 会话失效的超时时间（单位：ms）
	cfg_session_timeout_auth      = int64(5e9) // 连接成功后，等待验证的时间（单位：ms）,负数表示没有超时限制

	cfg_room_record_flag = false // 房间是否录像（false：不录像）
	cfg_room_record_dir  = "./room_record"

	cfg_frame_num_per_second        = 30            // 帧同步：每秒多少帧
	cfg_frame_timeout_void_command  = int64(30e6)   // 帧同步：最大空指令时间间隔
	cfg_frame_deviation_lower_limit = int64(-100e6) // 帧同步：帧误差下限（毫秒）
	cfg_frame_deviation_upper_limit = int64(34e6)   // 帧同步：帧误差上限（毫秒）
	cfg_frame_delta_ini             = int32(1000)

	cfg_db_cluster_node_total     = uint32(1)
	cfg_master_cluster_node_total = uint32(1)

	cfg_grpc_client_timeout = int64(1e6) * 1000
	cfg_task_interval       = int64(1e9) * 10

	cfg_player_data_expiration_in_hour    = 72 // redis缓存玩家数据的时间（单位：小时）
	cfg_player_data_save_interval_redis   = int64(1e9) * 60
	cfg_player_data_save_interval_leveldb = int64(1e9) * 60 * 5

	cfg_table_path = "./table"

	cfg_node_id_suffix = ""
)

var gMapConfig map[string]string

func init() {
	gMapConfig = make(map[string]string)
}

func get_config_from_consul() (bool, string) {
	dbCluster.NodeTotal = 0
	dbCluster.Nodes = make([]common.NodeInfo, cfg_db_cluster_node_total)
	dbCluster.NodeValues = make([]uint16, cfg_db_cluster_node_total)
	dbCluster.Conns = make([]*grpc.ClientConn, cfg_db_cluster_node_total)
	dbCluster.Clients = make([]pb.DbServiceClient, cfg_db_cluster_node_total)

	masterCluster.NodeTotal = 0
	masterCluster.Nodes = make([]common.NodeInfo, cfg_master_cluster_node_total)
	masterCluster.Conns = make([]*grpc.ClientConn, cfg_master_cluster_node_total)
	masterCluster.Clients = make([]pb.MasterServiceClient, cfg_master_cluster_node_total)

	client, err := consulapi.NewClient(consulapi.DefaultConfig())
	if err != nil {
		return false, err.Error()
	}
	if services, err := client.Agent().Services(); err != nil {
		return false, err.Error()
	} else {
		for _, v := range services {
			if common.CheckString(db_proxy_tag, v.Tags) {
				if dbCluster.NodeTotal < cfg_db_cluster_node_total {
					dbCluster.Nodes[dbCluster.NodeTotal].Id = v.ID
					dbCluster.Nodes[dbCluster.NodeTotal].Addr = fmt.Sprintf("%s:%d", v.Address, v.Port)
					dbCluster.NodeTotal++
				}
			} else if common.CheckString(master_tag, v.Tags) {
				if masterCluster.NodeTotal < cfg_master_cluster_node_total {
					masterCluster.Nodes[masterCluster.NodeTotal].Id = v.ID
					masterCluster.Nodes[masterCluster.NodeTotal].Addr = fmt.Sprintf("%s:%d", v.Address, v.Port)
					masterCluster.NodeTotal++
				}
			}
		}
		if dbCluster.NodeTotal != cfg_db_cluster_node_total || masterCluster.NodeTotal != cfg_master_cluster_node_total {
			return false, fmt.Sprintf("config_db_cluster_total = %d, consul_db_cluster_total = %d, config_master_cluster_total = %d, consul_master_cluster_total = %d, config_redis_cluster_total = %d, consul_redis_cluster_total = %d,",
				cfg_db_cluster_node_total, dbCluster.NodeTotal, cfg_master_cluster_node_total, masterCluster.NodeTotal)
		}
	}

	return true, ""
}

func LoadConfig(f string) (bool, string) {

	cfg, err := config.ReadDefault(f)
	if err != nil {
		return false, "Fail to read config file. " + err.Error()
	}
	for _, sec := range cfg.Sections() {
		opts, _ := cfg.SectionOptions(sec)
		for _, opt := range opts {
			gMapConfig[string(sec+"."+opt)], _ = cfg.String(sec, opt)
		}
	}

	// 把配置读到变量里
	if str, ok := gMapConfig["server.node_id"]; ok {
		cfg_node_id = str
	}
	if str, ok := gMapConfig["server.node_name"]; ok {
		cfg_node_name = str
	}
	if str, ok := gMapConfig["server.node_tags"]; ok {
		cfg_node_tags = strings.Split(str, ",")
	}
	if str, ok := gMapConfig["server.node_check_port"]; ok {
		cfg_node_check_port = str
	}
	if str, ok := gMapConfig["server.node_check_url"]; ok {
		cfg_node_check_url = str
	}
	if str, ok := gMapConfig["server.node_check_timeout"]; ok {
		cfg_node_check_timeout = str
	}
	if str, ok := gMapConfig["server.node_check_interval"]; ok {
		cfg_node_check_interval = str
	}
	if str, ok := gMapConfig["server.node_check_deregister_timeout"]; ok {
		cfg_node_check_deregister_timeout = str
	}
	if str, ok := gMapConfig["server.node_addr"]; ok {
		cfg_node_addr = str
	}
	if str, ok := gMapConfig["server.node_port"]; ok {
		cfg_node_port = str
	}
	if str, ok := gMapConfig["server.log_dir"]; ok {
		cfg_log_dir = str
	}
	if str, ok := gMapConfig["server.log_custom_level"]; ok {
		cfg_log_custom_level = str
	}
	if str, ok := gMapConfig["server.session_timeout_heartbeat"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_session_timeout_heartbeat = (int64)(v)
	}
	if str, ok := gMapConfig["server.session_timeout_auth"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_session_timeout_auth = (int64)(v)
	}
	if str, ok := gMapConfig["server.server_addr_consul_agent"]; ok {
		cfg_server_addr_consul_agent = str
	}
	if str, ok := gMapConfig["server.server_addr_redis"]; ok {
		cfg_server_addr_redis = str
	}
	if str, ok := gMapConfig["server.server_pwd_redis"]; ok {
		cfg_server_pwd_redis = str
	}
	if str, ok := gMapConfig["server.io_timeout"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_io_timeout = (int64)(v)
	}
	if str, ok := gMapConfig["server.io_pool_size"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_io_pool_size = (int)(v)
	}
	if str, ok := gMapConfig["server.unauth_io_coroutine_total"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_unauth_io_coroutine_total = (int)(v)
	}
	if str, ok := gMapConfig["server.db_cluster_node_total"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_db_cluster_node_total = uint32(v)
	}
	if str, ok := gMapConfig["server.master_cluster_node_total"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_master_cluster_node_total = uint32(v)
	}
	if str, ok := gMapConfig["server.grpc_client_timeout"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_grpc_client_timeout = (int64)(v)
	}
	if str, ok := gMapConfig["server.player_data_expiration_in_hour"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_player_data_expiration_in_hour = v
	}
	if str, ok := gMapConfig["server.player_data_save_interval_redis"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_player_data_save_interval_redis = (int64)(v)
	}
	if str, ok := gMapConfig["server.player_data_save_interval_leveldb"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_player_data_save_interval_leveldb = (int64)(v)
	}

	// game
	if str, ok := gMapConfig["game.room_record_flag"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		if v == 0 {
			cfg_room_record_flag = false
		} else {
			cfg_room_record_flag = true
		}

	}
	if str, ok := gMapConfig["game.room_record_dir"]; ok {
		cfg_room_record_dir = str
	}
	if str, ok := gMapConfig["game.frame_num_per_second"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_frame_num_per_second = (int)(v)
	}
	if str, ok := gMapConfig["game.frame_timeout_void_command"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_frame_timeout_void_command = (int64)(v)
	}
	if str, ok := gMapConfig["game.frame_deviation_lower_limit"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_frame_deviation_lower_limit = (int64)(v)
	}
	if str, ok := gMapConfig["game.frame_deviation_upper_limit"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_frame_deviation_upper_limit = (int64)(v)
	}
	if str, ok := gMapConfig["game.table_path"]; ok {
		cfg_table_path = str
	}

	return true, ""
}
