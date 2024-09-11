package benchmark_grpc

import "testing"

func BenchmarkClientLocal(b *testing.B) {
	// init
	worker := &Worker{}
	i := 0
	limit := b.N
	end := make(chan struct{})

	// test sub-get-process
	//
	// 1. subscription: wait for notifications
	// 2. getter: get a value from the worker
	// 3. processing: call an operation based on the value
	worker.Subscribe(func() {
		// loop
		i++
		if i > limit {
			close(end)
			return
		}

		// value (getter)
		value := worker.GetValue()

		// call op from value (processing)
		switch value {
		case Value1:
			go worker.CallOp(Op1)
		case Value2:
			go worker.CallOp(Op2)
		case Value3:
			go worker.CallOp(Op3)
		default:
			// err
			b.Fatalf("Unknown value: %v", value)
		}
	})

	// reset the timer to exclude setup time
	b.ResetTimer()

	// start, wait and report
	worker.Start()
	<-end
	b.ReportAllocs()
}
