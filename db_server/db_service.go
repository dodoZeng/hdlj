package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/golang/glog"
	"github.com/syndtr/goleveldb/leveldb"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"roisoft.com/hdlj/common"
	pb "roisoft.com/hdlj/proto"
)

type db_service struct {
	dbAcc      *leveldb.DB
	dbGame     *leveldb.DB
	dbAccIndex *leveldb.DB

	wg   *sync.WaitGroup
	s    *grpc.Server
	stop bool
}

const (
	cfg_roisoft_db_node_prefix = "roisoft.db."
)

func NewDbService() *db_service {
	return &db_service{
		wg:   &sync.WaitGroup{},
		s:    grpc.NewServer(),
		stop: true,
	}
}

func (service *db_service) start_grpc_service(addr string) (bool, string) {

	if strings.HasPrefix(cfg_node_id, cfg_roisoft_db_node_prefix) {
		// account db
		db_file := fmt.Sprintf("%s/%d", cfg_db_dir, cfg_node_id_suffix)
		db_index_file := fmt.Sprintf("%s/%d/index", cfg_db_dir, cfg_node_id_suffix)

		db_file = common.GetAbsFilePath(db_file)
		db_index_file = common.GetAbsFilePath(db_index_file)

		os.MkdirAll(db_file, 0777)
		os.MkdirAll(db_index_file, 0777)

		if db, err := leveldb.OpenFile(db_file, nil); err != nil {
			return false, err.Error()
		} else {
			service.dbAcc = db
		}
		if db, err := leveldb.OpenFile(db_index_file, nil); err != nil {
			return false, err.Error()
		} else {
			service.dbAccIndex = db
		}

	} else {
		// game db
		db_file := fmt.Sprintf("%s/%d", cfg_db_dir, cfg_node_id_suffix)

		db_file = common.GetAbsFilePath(db_file)

		os.MkdirAll(db_file, 0777)

		if db, err := leveldb.OpenFile(db_file, nil); err != nil {
			return false, err.Error()
		} else {
			service.dbGame = db
		}
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return false, fmt.Sprintf("failed to listen: %v\n", err)
	}

	pb.RegisterDbServiceServer(service.s, service)
	reflection.Register(service.s)
	go func() {
		if err := service.s.Serve(lis); err != nil {
			glog.Errorf("failed to grpc serve: %v\n", err)
		}
	}()

	service.stop = false

	return true, ""
}

func (service *db_service) stop_grpc_service() {
	service.s.Stop()
	service.stop = true

	service.wg.Wait()
}

func (s *db_service) DbOperation(ctx context.Context, in *pb.DbRequest) (*pb.DbReply, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	var err error

	var opdb *leveldb.DB
	if in.Type == uint32(pb.OpType_Account) {
		opdb = s.dbAcc
	} else if in.Type == uint32(pb.OpType_Account_Index) {
		opdb = s.dbAccIndex
	} else if in.Type == uint32(pb.OpType_Game) {
		opdb = s.dbGame
	}

	if opdb == nil {
		err := fmt.Sprintf("invalid optype. [node_id = %s, OpType = %d]", cfg_node_id, in.Type)
		glog.Errorln(err)
		return nil, errors.New(err)
	}

	if in.Method == uint32(pb.OpMethod_GET) {
		data, err := opdb.Get(in.Key, nil)
		if err != nil {
			if err == leveldb.ErrNotFound {
				return &pb.DbReply{}, nil
			} else {
				return nil, err
			}
		}
		return &pb.DbReply{Value: data}, nil
	} else if in.Method == uint32(pb.OpMethod_PUT) {
		err = opdb.Put(in.Key, in.Value, nil)
		if err != nil {
			return nil, err
		}
	} else if in.Method == uint32(pb.OpMethod_DEL) {
		err = opdb.Delete(in.Key, nil)
		if err != nil {
			return nil, err
		}
	} else if in.Method == uint32(pb.OpMethod_CHK) {
		var b [1]byte
		if _, err := opdb.Get(in.Key, nil); err == nil {
			b[0] = 1
		}
		return &pb.DbReply{Value: b[0:]}, nil
	} else {
		err := fmt.Sprintf("invalid method. [node_id = %s, Method = %d]", cfg_node_id, in.Method)
		glog.Errorln(err)
		return nil, errors.New(err)
	}

	return &pb.DbReply{}, nil
}

func (s *db_service) DbOperations(stream pb.DbService_DbOperationsServer) error {
	s.wg.Add(1)
	defer s.wg.Done()

	err := fmt.Sprintf("invalid grpc method. [node_id = %s]", cfg_node_id)
	glog.Errorln(err)
	return errors.New(err)
}
