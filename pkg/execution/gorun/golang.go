package gorun

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/stroppy-io/stroppy-core/pkg/plugins/driver"
	stroppy "github.com/stroppy-io/stroppy-core/pkg/proto"
	"github.com/stroppy-io/stroppy-core/pkg/shutdown"
	"github.com/stroppy-io/stroppy-core/pkg/utils"
)

func countQueries(descr *stroppy.StepQueryDescriptor) int {
	switch descr.GetType().(type) {
	case *stroppy.StepQueryDescriptor_Query:
		return int(descr.GetQuery().GetCount()) //nolint: gosec // allow
	case *stroppy.StepQueryDescriptor_CreateTable:
		return len(descr.GetCreateTable().GetTableIndexes()) + 1
	default:
		return 0
	}
}

var (
	ErrRunContextNil = errors.New("run context is nil")
	ErrStepNil       = errors.New("step is nil")
	ErrConfigNil     = errors.New("config is nil")
)

func RunStep(ctx context.Context, logger *zap.Logger, runContext *stroppy.StepContext) error {
	if runContext == nil {
		return ErrRunContextNil
	}

	if runContext.GetStep() == nil {
		return ErrStepNil
	}

	if runContext.GetConfig().GetGoExecutor() == nil {
		return ErrConfigNil
	}

	drv, drvCancelFn, err := driver.ConnectToPlugin(runContext.GetConfig(), logger)
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
		int(runContext.GetConfig().GetGoExecutor().GetGoMaxProc()), //nolint: gosec // allow
		runContext.GetConfig().GetGoExecutor().GetCancelOnError(),
	)

	for _, queryDesc := range runContext.GetStep().GetQueries() {
		stepPool.Go(func(ctx context.Context) error {
			queries, err := drv.BuildQueries(ctx, &stroppy.BuildQueriesContext{
				Context: runContext,
				Query:   queryDesc,
			})
			if err != nil {
				return err
			}

			queryPool := utils.NewAsyncerFromExecType(ctx,
				queryDesc.GetAsync(),
				countQueries(queryDesc),
				runContext.GetConfig().GetGoExecutor().GetCancelOnError(),
			)

			for _, query := range queries.GetQueries() {
				queryPool.Go(func(ctx context.Context) error {
					return drv.RunQuery(ctx, query)
				})
			}

			return queryPool.Wait()
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
