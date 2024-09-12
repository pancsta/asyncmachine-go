package benchmark_grpc

import (
	"context"
	"log"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/pancsta/asyncmachine-go/examples/benchmark_grpc/worker_proto"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

func BenchmarkClientGrpc(b *testing.B) {
	// init
	ctx := context.Background()
	worker := &Worker{}
	limit := b.N
	end := make(chan struct{})
	calls := 0

	// init grpc server
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		b.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	service := NewWorkerServiceServer(worker)
	pb.RegisterWorkerServiceServer(s, service)
	reflection.Register(s)
	go s.Serve(lis)
	defer lis.Close()
	l("test", "grpc server started")
	serverAddr := lis.Addr().String()

	// monitor traffic
	counterListener := utils.RandListener("localhost")
	connAddr := counterListener.Addr().String()
	counter := make(chan int64, 1)
	go arpc.TrafficMeter(counterListener, serverAddr, counter, end)

	// init grpc client
	conn, err := grpc.NewClient(connAddr, grpc.WithInsecure())
	if err != nil {
		b.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	client := pb.NewWorkerServiceClient(conn)
	l("test", "grpc client started")

	// test subscribe-get-process
	//
	// 1. subscription: wait for notifications
	// 2. getter: get the value from the source
	// 3. processing: call an operation based on the value
	calls++
	stream, err := client.Subscribe(ctx, &pb.Empty{})
	if err != nil {
		b.Fatalf("Subscribe failed: %v", err)
	}

	go func() {
		for i := 0; i <= limit; i++ {

			// wait for notification (subscription)
			_, err := stream.Recv()
			if err != nil {
				log.Fatalf("Failed to receive a notification: %v", err)
			}

			// value (getter)
			calls++
			respValue, err := client.GetValue(ctx, &pb.Empty{})
			if err != nil {
				log.Fatalf("GetValue failed: %v", err)
			}

			// call op from value (processing)
			calls++
			switch Value(respValue.Value) {
			case Value1:
				_, err = client.CallOp(ctx, &pb.CallOpRequest{Op: int32(Op1)})
			case Value2:
				_, err = client.CallOp(ctx, &pb.CallOpRequest{Op: int32(Op2)})
			case Value3:
				_, err = client.CallOp(ctx, &pb.CallOpRequest{Op: int32(Op3)})
			default:
				// err
				b.Fatalf("Unknown value: %v", respValue.Value)
			}
			if err != nil {
				b.Fatalf("CallOp failed: %v", err)
			}
		}

		// exit
		close(end)
	}()

	// reset the timer to exclude setup time
	b.ResetTimer()

	// start, wait and report
	calls++
	_, err = client.Start(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("Start failed: %v", err)
	}
	<-end
	b.ReportAllocs()
	p := message.NewPrinter(language.English)
	b.Log(p.Sprintf("Transferred: %d bytes", <-counter))
	b.Log(p.Sprintf("Calls: %d", calls+service.calls))
	b.Log(p.Sprintf("Errors: %d", worker.ErrCount))
	b.Log(p.Sprintf("Completions: %d", worker.SuccessCount))

	assert.Equal(b, 0, worker.ErrCount)
	assert.Greater(b, worker.SuccessCount, 0)
}
