package common

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)
import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/go-redis/redis"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	pb "roisoft.com/hdlj/proto"
)

const (
	Log_Info_Level_1 = 1
	Log_Info_Level_2 = 2
	Log_Info_Level_3 = 3

	NodeId_Roisoft_Master    = "roisoft.master"
	Redis_Key_Captcha_Format = "hdlj.captcha.token.%s"
)

type NodeInfo struct {
	Id        string
	Addr      string
	Port      int
	Name      string // 节点名称
	Load      int    // 负载量
	Available bool   // 是否可用
}

type DbCluster struct {
	Nodes      []NodeInfo
	NodeValues []uint16 // 数值从小到大排序
	NodeTotal  uint32
	Conns      []*grpc.ClientConn
	Clients    []pb.DbServiceClient
}

type MasterCluster struct {
	Nodes     []NodeInfo
	NodeTotal uint32
	Conns     []*grpc.ClientConn
	Clients   []pb.MasterServiceClient

	LeaderClient pb.MasterServiceClient
	LeaderConn   *grpc.ClientConn
	LeaderAddr   string
}

type RedisCluster struct {
	Nodes     []NodeInfo
	NodeTotal uint32
	Conns     []*redis.Client
}

func GetNodeValueByPlayerId(player_id uint64) uint16 {
	n := uint16(player_id % 16)
	offset := uint16((1 << 16) / 16)
	return uint16((player_id + uint64(n*offset)) % (1 << 16))
}

func GetNodeValueByString(str string) uint16 {
	h := sha256.Sum256([]byte(str))
	data := binary.BigEndian.Uint32(h[:])
	return uint16(data % (1 << 16))
}

// 根据ID数值，把节点按照Value从小到大排序
func (cluster *DbCluster) Sort() bool {
	if cluster.NodeTotal <= 0 {
		return false
	}

	// 从节点Id得到节点值
	for i := uint32(0); i < cluster.NodeTotal; i++ {
		node_id := cluster.Nodes[i].Id
		pos := strings.LastIndex(node_id, ".")
		if pos == -1 {
			pos = 0
		} else {
			pos++
		}

		if n, err := strconv.Atoi(node_id[pos:]); err != nil {
			return false
		} else if n < 0 || n >= (1<<16) {
			return false
		} else {
			cluster.NodeValues[i] = uint16(n)
		}
	}

	// 排序
	for i := uint32(0); i < cluster.NodeTotal-1; i++ {
		i_min := uint32(0)
		for j := uint32(i + 1); j < cluster.NodeTotal; j++ {
			if cluster.NodeValues[j] < cluster.NodeValues[i] {
				i_min = j
			}
		}
		if i != i_min {
			tmp_node := cluster.Nodes[i]
			tmp_node_value := cluster.NodeValues[i]
			tmp_cnn := cluster.Conns[i]

			cluster.Nodes[i] = cluster.Nodes[i_min]
			cluster.NodeValues[i] = cluster.NodeValues[i_min]
			cluster.Conns[i] = cluster.Conns[i_min]

			cluster.Nodes[i_min] = tmp_node
			cluster.NodeValues[i_min] = tmp_node_value
			cluster.Conns[i_min] = tmp_cnn
		}
	}

	return true
}

// 根据playerId获取一个数据库集群的节点
func (cluster *DbCluster) GetNodeIndexByPlayerId(player_id uint64) uint32 {

	node_value := GetNodeValueByPlayerId(player_id)
	for i := uint32(0); i < cluster.NodeTotal; i++ {
		if node_value <= cluster.NodeValues[i] {
			return i
		}
	}
	return 0
}

// 根据字符串获取一个数据库集群的节点
func (cluster *DbCluster) GetNodeIndexByString(str string) uint32 {

	node_value := GetNodeValueByString(str)
	for i := uint32(0); i < cluster.NodeTotal; i++ {
		if node_value <= cluster.NodeValues[i] {
			return i
		}
	}
	return 0
}

func (cluster *DbCluster) GetNodeByPlayerId(player_id uint64) pb.DbServiceClient {
	return cluster.GetNodeByIndex(cluster.GetNodeIndexByPlayerId(player_id))
}

func (cluster *DbCluster) GetNodeByString(str string) pb.DbServiceClient {
	return cluster.GetNodeByIndex(cluster.GetNodeIndexByString(str))
}

func (cluster *DbCluster) GetNodeByIndex(index uint32) pb.DbServiceClient {
	conn := cluster.Conns[index]
	c := cluster.Clients[index]

	// check connection & reconnect
	if conn.GetState() == connectivity.TransientFailure ||
		conn.GetState() == connectivity.Shutdown {

		addr := cluster.Nodes[index].Addr
		if new_conn, err := grpc.Dial(addr, grpc.WithInsecure()); err != nil {
			fmt.Printf("fail to recconect db server. [addr = %s, err = %v]\n", addr, err)
			return nil
		} else {
			new_client := pb.NewDbServiceClient(new_conn)

			cluster.Conns[index] = new_conn
			cluster.Clients[index] = new_client

			c = new_client
		}
	}

	return c
}

func (cluster *MasterCluster) GetNodeByPlayerId(player_id uint64) pb.MasterServiceClient {
	index := uint32(player_id % uint64(cluster.NodeTotal))
	return cluster.GetNodeByIndex(index)
}

func (cluster *MasterCluster) GetNodeByIndex(index uint32) pb.MasterServiceClient {
	conn := cluster.Conns[index]
	c := cluster.Clients[index]

	// check connection & reconnect
	if conn.GetState() == connectivity.TransientFailure ||
		conn.GetState() == connectivity.Shutdown {

		addr := cluster.Nodes[index].Addr
		if new_conn, err := grpc.Dial(addr, grpc.WithInsecure()); err != nil {
			fmt.Printf("fail to recconect master server. [addr = %s, err = %v]\n", addr, err)
			return nil
		} else {
			new_client := pb.NewMasterServiceClient(new_conn)

			cluster.Conns[index] = new_conn
			cluster.Clients[index] = new_client
			if addr == cluster.LeaderAddr {
				cluster.LeaderConn = new_conn
				cluster.LeaderClient = new_client
			}

			c = new_client
		}
	}

	return c
}

func (cluster *MasterCluster) GetLeader() pb.MasterServiceClient {
	return cluster.LeaderClient
}

func (cluster *RedisCluster) GetNodeByString(str string) *redis.Client {
	node_value := uint32(GetNodeValueByString(str))
	index := node_value % cluster.NodeTotal
	c := cluster.Conns[index]

	// check connection & reconnect
	if err := c.Ping().Err(); err != nil {
		is_ok := false
		new_c := redis.NewClient(&redis.Options{
			Addr:        cluster.Nodes[index].Addr,
			Password:    "",
			DB:          0, // use default DB
			MaxRetries:  3,
			DialTimeout: time.Second * 5,
		})
		if err := new_c.Ping().Err(); err == nil {
			cluster.Conns[index] = new_c
			c = new_c
			is_ok = true
		}
		if !is_ok {
			return nil
		}
	}

	return c
}

func CheckString(str string, strArray []string) bool {
	for _, v := range strArray {
		if strings.EqualFold(str, v) {
			return true
		}
	}
	return false
}

func CheckFileIsExist(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
}

func GetAbsFilePath(fp string) string {
	if filepath.IsAbs(fp) {
		return fp
	}

	ex, _ := os.Executable()
	base := filepath.Dir(ex)
	return filepath.Join(base, fp)
}

func MakeToken() string {
	crutime := time.Now().Unix()
	h := md5.New()

	io.WriteString(h, strconv.FormatInt(crutime, 10))

	return fmt.Sprintf("%x", h.Sum(nil))
}
