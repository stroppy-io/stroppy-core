package errchan

import (
	"errors"
)

type Chan[T any] = chan *ChanResult[T]

type ChanResult[T any] struct {
	data  *T
	Error error
}

func (res *ChanResult[T]) IsError() bool {
	return res.Error != nil
}

func (res *ChanResult[T]) Unwrap() *T {
	if res.IsError() {
		panic(res.Error)
	}

	return res.data
}

func (res *ChanResult[T]) Get() (*T, error) {
	return res.data, res.Error
}

func Send[T any](ch Chan[T], data *T, err error) {
	ch <- &ChanResult[T]{data: data, Error: err}
}

func Close[T any](ch Chan[T]) {
	close(ch)
}

var ErrReceiveClosed = errors.New("errchan: receive from closed channel")

func Receive[T any](ch Chan[T]) (*T, error) {
	rec, ok := <-ch
	if !ok {
		return nil, ErrReceiveClosed
	}

	return rec.Get()
}

func Collect[T any](ch Chan[T]) ([]*T, error) {
	result := make([]*T, 0)

	for {
		data, err := Receive[T](ch)
		if err != nil {
			if errors.Is(err, ErrReceiveClosed) {
				return result, nil
			}

			return nil, err
		}

		result = append(result, data)
	}
}
