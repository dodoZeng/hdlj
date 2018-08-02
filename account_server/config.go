package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)
import (
	"github.com/go-redis/redis"
	consulapi "github.com/hashicorp/consul/api"
	"google.golang.org/grpc"
	"roisoft.com/hdlj/common"
	"roisoft.com/hdlj/common/config"
	pb "roisoft.com/hdlj/proto"
)

const (
	db_proxy_tag = "roisoft.db.cluster"
	master_tag   = "hdlj.master.cluster"
	redis_tag    = "hdlj.redis.cluster"
)

var (
	cfg_file = "./account_server.cfg"

	cfg_node_id                       = "hdlj.account.1"
	cfg_node_name                     = "hdlj.account.1"
	cfg_node_tags                     = []string{"hdlj.account.cluster"}
	cfg_node_check_port               = "8001"
	cfg_node_check_url                = "/check"
	cfg_node_check_timeout            = "5s"
	cfg_node_check_interval           = "10s"
	cfg_node_check_deregister_timeout = "30s"
	cfg_node_addr                     = "0.0.0.0"
	cfg_node_port                     = "50001"

	cfg_server_addr_consul_agent = "0.0.0.0:8500"
	cfg_server_addr_redis        = "0.0.0.0:6379"
	cfg_server_pwd_redis         = ""

	cfg_log_custom_level = "3"
	cfg_log_dir          = "./log"

	cfg_client_pool_size          = 10
	cfg_db_cluster_node_total     = uint32(1)
	cfg_master_cluster_node_total = uint32(1)
	cfg_redis_cluster_node_total  = uint32(1)

	cfg_grpc_client_timeout = int64(1e9)

	cfg_pwd_input_total = 2
	cfg_need_token      = false

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

	redisCluster.NodeTotal = 0
	redisCluster.Nodes = make([]common.NodeInfo, cfg_redis_cluster_node_total)
	redisCluster.Conns = make([]*redis.Client, cfg_redis_cluster_node_total)

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
			} else if v.ID == common.NodeId_Roisoft_Master {
				masterCluster.LeaderAddr = fmt.Sprintf("%s:%d", v.Address, v.Port)
			} else if common.CheckString(master_tag, v.Tags) {
				if masterCluster.NodeTotal < cfg_master_cluster_node_total {
					masterCluster.Nodes[masterCluster.NodeTotal].Id = v.ID
					masterCluster.Nodes[masterCluster.NodeTotal].Addr = fmt.Sprintf("%s:%d", v.Address, v.Port)
					masterCluster.NodeTotal++
				}
			} else if common.CheckString(redis_tag, v.Tags) {
				if redisCluster.NodeTotal < cfg_redis_cluster_node_total {
					redisCluster.Nodes[redisCluster.NodeTotal].Id = v.ID
					redisCluster.Nodes[redisCluster.NodeTotal].Addr = fmt.Sprintf("%s:%d", v.Address, v.Port)
					redisCluster.NodeTotal++
				}
			}
		}
		if dbCluster.NodeTotal != cfg_db_cluster_node_total || len(masterCluster.LeaderAddr) <= 0 || masterCluster.NodeTotal != cfg_master_cluster_node_total || redisCluster.NodeTotal != cfg_redis_cluster_node_total {
			return false, fmt.Sprintf("leader_master = %s, config_db_cluster_total = %d, consul_db_cluster_total = %d, config_master_cluster_total = %d, consul_master_cluster_total = %d, config_redis_cluster_total = %d, consul_redis_cluster_total = %d",
				cfg_db_cluster_node_total, dbCluster.NodeTotal, cfg_master_cluster_node_total, masterCluster.NodeTotal, cfg_redis_cluster_node_total, redisCluster.NodeTotal)
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
	if str, ok := gMapConfig["server.server_addr_consul_agent"]; ok {
		cfg_server_addr_consul_agent = str
	}
	if str, ok := gMapConfig["server.server_addr_redis"]; ok {
		cfg_server_addr_redis = str
	}
	if str, ok := gMapConfig["server.server_pwd_redis"]; ok {
		cfg_server_pwd_redis = str
	}
	if str, ok := gMapConfig["server.client_pool_size"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_client_pool_size = v
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
	if str, ok := gMapConfig["server.redis_cluster_node_total"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_redis_cluster_node_total = uint32(v)
	}
	if str, ok := gMapConfig["server.grpc_client_timeout"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_grpc_client_timeout = (int64)(v)
	}

	// account
	if str, ok := gMapConfig["account.pwd_input_total"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_pwd_input_total = v
	}
	if str, ok := gMapConfig["account.need_token"]; ok {
		v, err := strconv.ParseBool(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_need_token = v
	}

	return true, ""
}
