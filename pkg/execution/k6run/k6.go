package k6run

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/stroppy-io/stroppy-core/pkg/logger"
	stroppy "github.com/stroppy-io/stroppy-core/pkg/proto"
	"github.com/stroppy-io/stroppy-core/pkg/shutdown"
	utils2 "github.com/stroppy-io/stroppy-core/pkg/utils"
)

func runK6Binary(
	_ context.Context,
	lg *zap.Logger,
	binaryPath string,
	args ...string,
) error {
	binExec := exec.Command(binaryPath, args...)
	binExec.Stdout = os.Stdout
	binExec.Stderr = os.Stderr

	if err := binExec.Start(); err != nil {
		return fmt.Errorf("fail run k6 binary %s: %w", binaryPath, err)
	}

	shutdown.RegisterFn(func() {
		// Send a termination signal to the process
		if err := binExec.Process.Signal(syscall.SIGTERM); err != nil {
			lg.Error(
				"error sending SIGTERM to k6 binary",
				zap.String("binary_path", binaryPath),
				zap.Error(err),
			)
		}
		// Wait for the process to terminate gracefully
		time.Sleep(1 * time.Second)

		if binExec.ProcessState == nil || !binExec.ProcessState.Exited() {
			lg.Error(
				"k6 binary did not terminate gracefully, forcefully killing...",
				zap.String("binary_path", binaryPath),
			)

			if err := binExec.Process.Kill(); err != nil {
				lg.Error(
					"Error forcefully killing %s: %s",
					zap.String("binary_path", binaryPath),
					zap.Error(err),
				)
			}
		}
	})

	return binExec.Wait()
}

var (
	ErrRunContextIsNil = errors.New("run context is nil")
	ErrStepIsNil       = errors.New("step is nil")
	ErrConfigIsNil     = errors.New("config is nil")
)

func RunStep(
	ctx context.Context,
	lg *zap.Logger,
	runContext *stroppy.StepContext,
) error {
	currentLogger := lg.Named("K6Executor")

	if runContext == nil {
		return ErrRunContextIsNil
	}

	if runContext.GetStep() == nil {
		return ErrStepIsNil
	}

	if runContext.GetConfig().GetK6Executor() == nil {
		return ErrConfigIsNil
	}

	if runContext.GetStep().GetAsync() {
		queries := slices.Clone(runContext.GetStep().GetQueries())
		rand.Shuffle(len(queries), func(i, j int) {
			queries[i], queries[j] = queries[j], queries[i]
		})

		runContext.GetStep().Queries = queries
	}

	contextStr, err := protojson.Marshal(runContext)
	if err != nil {
		return err
	}

	baseArgs := []string{
		"run",
		runContext.GetConfig().GetK6Executor().GetK6ScriptPath(),
		"-econtext=" + string(contextStr),
	}

	if runContext.GetConfig().GetK6Executor().GetOtlpExport() != nil {
		os.Setenv("K6_OTEL_GRPC_EXPORTER_INSECURE", "true")
		os.Setenv(
			"K6_OTEL_METRIC_PREFIX",
			utils2.StringOrDefault(
				runContext.GetConfig().GetK6Executor().GetOtlpExport().GetOtlpMetricsPrefix(),
				"k6_",
			),
		)
		os.Setenv(
			"K6_OTEL_SERVICE_NAME",
			fmt.Sprintf("%s_%s",
				runContext.GetBenchmark().GetName(),
				runContext.GetStep().GetName()),
		)
		os.Setenv("K6_OTEL_GRPC_EXPORTER_ENDPOINT", utils2.StringOrDefault(
			runContext.GetConfig().GetK6Executor().GetOtlpExport().GetOtlpGrpcEndpoint(),
			"localhost:4317",
		))

		baseArgs = append(baseArgs, "--out", "experimental-opentelemetry")
	}

	baseArgs = append(
		baseArgs,
		runContext.GetConfig().GetK6Executor().GetK6BinaryArgs()...,
	)
	currentLogger.Debug("Running K6", zap.String("args", fmt.Sprintf("%v", baseArgs)))
	logger.SetLoggerEnv(
		logger.LevelFromProtoConfig(runContext.GetConfig().GetLogger().GetLogLevel()),
		logger.ModeFromProtoConfig(runContext.GetConfig().GetLogger().GetLogMode()),
	)

	return runK6Binary(
		ctx,
		currentLogger,
		runContext.GetConfig().GetK6Executor().GetK6BinaryPath(),
		baseArgs...,
	)
}
