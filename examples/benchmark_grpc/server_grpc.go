package benchmark_grpc

import (
	"context"
	"sync"

	pb "github.com/pancsta/asyncmachine-go/examples/benchmark_grpc/proto"
)

type WorkerServiceServer struct {
	pb.UnimplementedWorkerServiceServer
	mu         sync.Mutex
	worker     *Worker
	subscriber chan struct{}
	ready      chan struct{}
	calls      int
}

func NewWorkerServiceServer(worker *Worker) *WorkerServiceServer {
	s := &WorkerServiceServer{
		worker: worker,
		ready:  make(chan struct{}),
	}

	worker.Subscribe(func() {
		s.subscriber <- struct{}{}
	})

	return s
}

func (s *WorkerServiceServer) CallOp(ctx context.Context, req *pb.CallOpRequest) (*pb.CallOpResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	op := Op(req.Op)
	l("grpc-server", "op: %v", op)

	s.worker.CallOp(op)

	return &pb.CallOpResponse{Success: true}, nil
}

func (s *WorkerServiceServer) Subscribe(req *pb.Empty, stream pb.WorkerService_SubscribeServer) error {
	l("grpc-server", "Subscribe")
	ch := make(chan struct{}, 10)
	s.mu.Lock()
	s.subscriber = ch
	close(s.ready)
	s.mu.Unlock()

	for range ch {
		l("grpc-server", "notify")
		s.calls++
		if err := stream.Send(&pb.Empty{}); err != nil {
			return err
		}
	}

	return nil
}

func (s *WorkerServiceServer) GetValue(ctx context.Context, req *pb.Empty) (*pb.GetValueResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	l("grpc-server", "GetValue")

	return &pb.GetValueResponse{Value: int32(s.worker.GetValue())}, nil
}

func (s *WorkerServiceServer) Start(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	<-s.ready

	s.mu.Lock()
	defer s.mu.Unlock()

	l("grpc-server", "Start")
	s.worker.Start()

	return &pb.Empty{}, nil
}
