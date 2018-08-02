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
	"roisoft.com/hdlj/common"
	"roisoft.com/hdlj/common/config"
)

const (
	redis_tag = "hdlj.redis.cluster"
)

var (
	cfg_file = "./web_server.cfg"

	cfg_node_id                       = "hdlj.web.captcha"
	cfg_node_name                     = "hdlj.web.captcha"
	cfg_node_tags                     = []string{"hdlj.web.captcha"}
	cfg_node_check_port               = "8080"
	cfg_node_check_url                = "/check"
	cfg_node_check_timeout            = "5s"
	cfg_node_check_interval           = "10s"
	cfg_node_check_deregister_timeout = "30s"
	cfg_node_addr                     = "0.0.0.0"
	cfg_node_port                     = "8080"

	cfg_server_addr_consul_agent = "0.0.0.0:8500"
	cfg_server_addr_redis        = "0.0.0.0:6379"
	cfg_server_pwd_redis         = ""

	cfg_log_custom_level = "3"
	cfg_log_dir          = "./log"

	cfg_captcha_expiration = int64(1e9) * 60

	cfg_redis_cluster_node_total = uint32(1)

	cfg_node_id_suffix = ""
)

var gMapConfig map[string]string

func init() {
	gMapConfig = make(map[string]string)
}

func get_config_from_consul() (bool, string) {
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
		for k, v := range services {
			if common.CheckString(redis_tag, v.Tags) {
				if redisCluster.NodeTotal < cfg_redis_cluster_node_total {
					redisCluster.Nodes[redisCluster.NodeTotal].Id = k
					redisCluster.Nodes[redisCluster.NodeTotal].Addr = fmt.Sprintf("%s:%d", v.Address, v.Port)
					redisCluster.NodeTotal++
				}
			}
		}

		if redisCluster.NodeTotal != cfg_redis_cluster_node_total {
			return false, fmt.Sprintf("config_redis_cluster_total = %d, consul_redis_cluster_total = %d,", cfg_redis_cluster_node_total, redisCluster.NodeTotal)
		} else {
			// connect to redis
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
	if str, ok := gMapConfig["server.redis_cluster_node_total"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_redis_cluster_node_total = uint32(v)
	}

	// web
	if str, ok := gMapConfig["web.captcha_expiration"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_captcha_expiration = (int64)(v)
	}
	return true, ""
}
