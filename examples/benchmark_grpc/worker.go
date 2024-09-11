package benchmark_grpc

import (
	"log"
	"math/rand"
	"os"
)

type Op int

const (
	Op1 Op = iota + 1
	Op2
	Op3
)

type Value int

const (
	Value1 Value = iota + 1
	Value2
	Value3
)

type Worker struct {
	ErrCount     int
	SuccessCount int
	value        Value
	evListener   func()
}

func (s *Worker) Start() {
	s.CallOp(Op(rand.Intn(3) + 1))
}

func (s *Worker) Subscribe(lis func()) {
	l("worker", "Subscribe")
	s.evListener = lis
}

func (s *Worker) GetValue() Value {
	return s.value
}

func (s *Worker) CallOp(op Op) {
	l("worker", "Call op: %v", op)

	// assert the value
	if s.value != 0 {
		switch op {
		case Op1:
			if s.value != Value1 {
				s.ErrCount++
			}
			s.SuccessCount++
		case Op2:
			if s.value != Value2 {
				s.ErrCount++
			}
			s.SuccessCount++
		case Op3:
			if s.value != Value3 {
				s.ErrCount++
			}
			s.SuccessCount++
		default:
			// err
			s.ErrCount++
		}
	}

	// create a rand value
	s.value = Value(rand.Intn(3) + 1)

	// call an event
	s.notify()
}

func (s *Worker) notify() {
	l("worker", "Notify")
	s.evListener()
}

func l(src, msg string, args ...any) {
	if os.Getenv("BENCH_DEBUG") == "" {
		return
	}
	log.Printf(src+": "+msg, args...)
}
