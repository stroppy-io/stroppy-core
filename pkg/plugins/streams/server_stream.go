package streams

import (
	"errors"
	"io"

	"google.golang.org/grpc"
)

type ServerStream[T any] interface {
	Send(data *T) error
	SendClose() error
	Recv() (*T, error)
}

type ServerStreamImpl[T any] struct {
	ch chan *T
}

func NewServerStreamImpl[T any]() *ServerStreamImpl[T] {
	return &ServerStreamImpl[T]{
		ch: make(chan *T),
	}
}

func (c ServerStreamImpl[T]) SendClose() error {
	close(c.ch)

	return nil
}

func (c ServerStreamImpl[T]) Send(t *T) error {
	c.ch <- t

	return nil
}

func (c ServerStreamImpl[T]) Recv() (*T, error) {
	next, ok := <-c.ch
	if !ok {
		return nil, io.EOF
	}

	return next, nil
}

var ErrSendNotSupportedByServerStreamingClient = errors.New("send not supported by server streaming client")

type serverStreamingClientReceiver[T any] struct {
	delegate grpc.ServerStreamingClient[T]
}

func (c serverStreamingClientReceiver[T]) Send(_ *T) error {
	return ErrSendNotSupportedByServerStreamingClient
}

func (c serverStreamingClientReceiver[T]) SendClose() error {
	return ErrSendNotSupportedByServerStreamingClient
}

func (c serverStreamingClientReceiver[T]) Recv() (*T, error) {
	return c.delegate.Recv()
}

func WrapServerStreamingClient[T any](stream grpc.ServerStreamingClient[T]) ServerStream[T] {
	return &serverStreamingClientReceiver[T]{delegate: stream}
}

func RestreamServerStreamingServer[T any](stream grpc.ServerStreamingServer[T], inner ServerStream[T]) error {
	for {
		data, err := inner.Recv()
		if err != nil {
			return err
		}

		err = stream.Send(data)
		if err != nil {
			return err
		}
	}
}
