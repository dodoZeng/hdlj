package main

import (
	"fmt"
	"net"
	"sync"
)
import (
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	pb "roisoft.com/hdlj/proto"
)

type grpc_service struct {
	wg   *sync.WaitGroup
	s    *grpc.Server
	stop bool
}

func init() {
}

func NewGrpcService() *grpc_service {
	return &grpc_service{
		wg:   &sync.WaitGroup{},
		s:    grpc.NewServer(),
		stop: true,
	}
}

func (service *grpc_service) start_grpc_service(addr string) (bool, string) {

	if ok, err := conn_to_all_server(); !ok {
		return false, err
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return false, fmt.Sprint("failed to listen: %v\n", err)
	}

	pb.RegisterAccountServiceServer(service.s, service)
	reflection.Register(service.s)
	go func() {
		defer glog.Infoln("routine exit: grpc.")
		defer fmt.Println("routine exit: grpc.")

		if err := service.s.Serve(lis); err != nil {
			glog.Errorf("failed to grpc serve: %v\n", err)
		}
	}()

	service.stop = false

	return true, ""
}

func (service *grpc_service) stop_grpc_service() {
	service.s.Stop()
	service.stop = true

	service.wg.Wait()
}

func (s *grpc_service) Signup(ctx context.Context, in *pb.SignupRequest) (*pb.ErrorInfo, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	return signup(in), nil
}

func (s *grpc_service) Signin(ctx context.Context, in *pb.SigninRequest) (*pb.SigninReply, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	return signin(in), nil
}

func (s *grpc_service) GetGameServerList(ctx context.Context, in *pb.SessionInfo) (*pb.GameServerList, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	return getGameServerList(in), nil
}

func (s *grpc_service) GetAccountData(ctx context.Context, in *pb.SessionInfo) (*pb.AccountData, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	return getAccountData(in), nil
}

func (s *grpc_service) CheckIDCard(ctx context.Context, in *pb.CheckIdCardRequest) (*pb.ErrorInfo, error) {
	s.wg.Add(1)
	defer s.wg.Done()

	return checkIDCard(in), nil
}
