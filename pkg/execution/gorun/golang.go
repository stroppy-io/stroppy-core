package gorun

import (
	"context"
	"errors"
	"io"

	"go.uber.org/zap"

	"github.com/stroppy-io/stroppy-core/pkg/plugins/driver"
	stroppy "github.com/stroppy-io/stroppy-core/pkg/proto"
	"github.com/stroppy-io/stroppy-core/pkg/shutdown"
	"github.com/stroppy-io/stroppy-core/pkg/utils"
)

var (
	ErrRunContextNil = errors.New("run context is nil")
	ErrStepNil       = errors.New("step is nil")
	ErrConfigNil     = errors.New("config is nil")
)

const minRunTxGoroutines = 2

func RunStep(
	ctx context.Context,
	logger *zap.Logger,
	runContext *stroppy.StepContext,
) error {
	if runContext == nil {
		return ErrRunContextNil
	}

	if runContext.GetStep() == nil {
		return ErrStepNil
	}

	if runContext.GetGlobalConfig().GetRun().GetGoExecutor() == nil {
		return ErrConfigNil
	}

	drv, drvCancelFn, err := driver.ConnectToPlugin(runContext.GetGlobalConfig().GetRun(), logger)
	if err != nil {
		return err
	}

	shutdown.RegisterFn(drvCancelFn)

	err = drv.Initialize(ctx, runContext)
	if err != nil {
		return err
	}

	cancelCtx, cancelFn := context.WithCancel(ctx)
	shutdown.RegisterFn(cancelFn)

	stepPool := utils.NewAsyncerFromExecType(
		cancelCtx,
		runContext.GetStep().GetAsync(),
		len(runContext.GetStep().GetUnits()),
		runContext.GetGlobalConfig().GetRun().GetGoExecutor().GetCancelOnError(),
	)
	for _, unitDesc := range runContext.GetStep().GetUnits() {
		stepPool.Go(func(ctx context.Context) error {
			return processUnitTransactions(ctx, drv, runContext, unitDesc)
		})
	}

	err = stepPool.Wait()
	if err != nil {
		return err
	}

	err = drv.Teardown(ctx)
	if err != nil {
		return err
	}

	return nil
}

func processUnitTransactions(
	ctx context.Context,
	drv driver.Plugin,
	runContext *stroppy.StepContext,
	unitDesc *stroppy.StepUnitDescriptor,
) error {
	transactionStream, err := drv.BuildTransactionsFromUnitStream(ctx, &stroppy.UnitBuildContext{
		Context: runContext,
		Unit:    unitDesc,
	})
	if err != nil {
		return err
	}

	unitPool := utils.NewAsyncerFromExecType(
		ctx,
		runContext.GetStep().GetAsync(),
		// TODO: need count already running pools and set max goroutines?
		max(
			int(runContext.GetGlobalConfig().GetRun().GetGoExecutor().GetGoMaxProc()), //nolint: gosec // allow
			minRunTxGoroutines,
		),
		runContext.GetGlobalConfig().GetRun().GetGoExecutor().GetCancelOnError(),
	)

	buffChan := make(chan *stroppy.DriverTransaction)

	unitPool.Go(func(_ context.Context) error {
		defer close(buffChan)

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				tx, err := transactionStream.Recv()
				if err != nil {
					if errors.Is(err, io.EOF) {
						return nil
					}

					return err
				}

				select {
				case buffChan <- tx:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	})

	for tx := range buffChan {
		unitPool.Go(func(ctx context.Context) error {
			return drv.RunTransaction(ctx, tx)
		})
	}

	return unitPool.Wait()
}
