// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.5.1
// - protoc             v5.28.1
// source: proto/worker.proto

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.64.0 or later.
const _ = grpc.SupportPackageIsVersion9

const (
	WorkerService_Start_FullMethodName     = "/proto.WorkerService/Start"
	WorkerService_CallOp_FullMethodName    = "/proto.WorkerService/CallOp"
	WorkerService_Subscribe_FullMethodName = "/proto.WorkerService/Subscribe"
	WorkerService_GetValue_FullMethodName  = "/proto.WorkerService/GetValue"
)

// WorkerServiceClient is the client API for WorkerService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type WorkerServiceClient interface {
	Start(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*Empty, error)
	CallOp(ctx context.Context, in *CallOpRequest, opts ...grpc.CallOption) (*CallOpResponse, error)
	Subscribe(ctx context.Context, in *Empty, opts ...grpc.CallOption) (grpc.ServerStreamingClient[Empty], error)
	GetValue(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*GetValueResponse, error)
}

type workerServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewWorkerServiceClient(cc grpc.ClientConnInterface) WorkerServiceClient {
	return &workerServiceClient{cc}
}

func (c *workerServiceClient) Start(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*Empty, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(Empty)
	err := c.cc.Invoke(ctx, WorkerService_Start_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *workerServiceClient) CallOp(ctx context.Context, in *CallOpRequest, opts ...grpc.CallOption) (*CallOpResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(CallOpResponse)
	err := c.cc.Invoke(ctx, WorkerService_CallOp_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *workerServiceClient) Subscribe(ctx context.Context, in *Empty, opts ...grpc.CallOption) (grpc.ServerStreamingClient[Empty], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &WorkerService_ServiceDesc.Streams[0], WorkerService_Subscribe_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[Empty, Empty]{ClientStream: stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type WorkerService_SubscribeClient = grpc.ServerStreamingClient[Empty]

func (c *workerServiceClient) GetValue(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*GetValueResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetValueResponse)
	err := c.cc.Invoke(ctx, WorkerService_GetValue_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WorkerServiceServer is the server API for WorkerService service.
// All implementations must embed UnimplementedWorkerServiceServer
// for forward compatibility.
type WorkerServiceServer interface {
	Start(context.Context, *Empty) (*Empty, error)
	CallOp(context.Context, *CallOpRequest) (*CallOpResponse, error)
	Subscribe(*Empty, grpc.ServerStreamingServer[Empty]) error
	GetValue(context.Context, *Empty) (*GetValueResponse, error)
	mustEmbedUnimplementedWorkerServiceServer()
}

// UnimplementedWorkerServiceServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedWorkerServiceServer struct{}

func (UnimplementedWorkerServiceServer) Start(context.Context, *Empty) (*Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Start not implemented")
}
func (UnimplementedWorkerServiceServer) CallOp(context.Context, *CallOpRequest) (*CallOpResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CallOp not implemented")
}
func (UnimplementedWorkerServiceServer) Subscribe(*Empty, grpc.ServerStreamingServer[Empty]) error {
	return status.Errorf(codes.Unimplemented, "method Subscribe not implemented")
}
func (UnimplementedWorkerServiceServer) GetValue(context.Context, *Empty) (*GetValueResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetValue not implemented")
}
func (UnimplementedWorkerServiceServer) mustEmbedUnimplementedWorkerServiceServer() {}
func (UnimplementedWorkerServiceServer) testEmbeddedByValue()                       {}

// UnsafeWorkerServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to WorkerServiceServer will
// result in compilation errors.
type UnsafeWorkerServiceServer interface {
	mustEmbedUnimplementedWorkerServiceServer()
}

func RegisterWorkerServiceServer(s grpc.ServiceRegistrar, srv WorkerServiceServer) {
	// If the following call pancis, it indicates UnimplementedWorkerServiceServer was
	// embedded by pointer and is nil.  This will cause panics if an
	// unimplemented method is ever invoked, so we test this at initialization
	// time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&WorkerService_ServiceDesc, srv)
}

func _WorkerService_Start_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).Start(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: WorkerService_Start_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).Start(ctx, req.(*Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _WorkerService_CallOp_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CallOpRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).CallOp(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: WorkerService_CallOp_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).CallOp(ctx, req.(*CallOpRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _WorkerService_Subscribe_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(Empty)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(WorkerServiceServer).Subscribe(m, &grpc.GenericServerStream[Empty, Empty]{ServerStream: stream})
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type WorkerService_SubscribeServer = grpc.ServerStreamingServer[Empty]

func _WorkerService_GetValue_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).GetValue(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: WorkerService_GetValue_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).GetValue(ctx, req.(*Empty))
	}
	return interceptor(ctx, in, info, handler)
}

// WorkerService_ServiceDesc is the grpc.ServiceDesc for WorkerService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var WorkerService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "proto.WorkerService",
	HandlerType: (*WorkerServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Start",
			Handler:    _WorkerService_Start_Handler,
		},
		{
			MethodName: "CallOp",
			Handler:    _WorkerService_CallOp_Handler,
		},
		{
			MethodName: "GetValue",
			Handler:    _WorkerService_GetValue_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Subscribe",
			Handler:       _WorkerService_Subscribe_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/worker.proto",
}
