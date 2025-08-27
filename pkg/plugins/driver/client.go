package driver

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/hashicorp/go-plugin"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/stroppy-io/stroppy-core/pkg/logger"
	stroppy "github.com/stroppy-io/stroppy-core/pkg/proto"
	"github.com/stroppy-io/stroppy-core/pkg/utils/errchan"
)

type client struct {
	lg          *zap.Logger
	protoClient stroppy.DriverPluginClient
}

const driverClientLoggerName = "driver-plugin-client"

func newDriverClient(protoClient stroppy.DriverPluginClient) *client {
	return &client{
		lg:          logger.NewStructLogger(driverClientLoggerName),
		protoClient: protoClient,
	}
}

func (d *client) Initialize(
	ctx context.Context,
	runContext *stroppy.StepContext,
) error {
	_, err := d.protoClient.Initialize(ctx, runContext)

	return err
}

func (d *client) BuildTransactionsFromUnit(
	ctx context.Context,
	buildUnitContext *stroppy.UnitBuildContext,
) (*stroppy.DriverTransactionList, error) {
	return d.protoClient.BuildTransactionsFromUnit(ctx, buildUnitContext)
}

func (d *client) BuildTransactionsFromUnitStream(
	ctx context.Context,
	buildUnitContext *stroppy.UnitBuildContext,
) (errchan.Chan[stroppy.DriverTransaction], error) {
	stream, err := d.protoClient.BuildTransactionsFromUnitStream(ctx, buildUnitContext)
	if err != nil {
		return nil, err
	}

	channel := make(errchan.Chan[stroppy.DriverTransaction])

	go func() {
		for {
			select {
			case <-ctx.Done():
				errchan.Close[stroppy.DriverTransaction](channel)

				return
			default:
				data, err := stream.Recv()
				if err != nil {
					if errors.Is(err, io.EOF) {
						return
					}

					errchan.Send[stroppy.DriverTransaction](channel, nil, err)

					return
				}

				errchan.Send[stroppy.DriverTransaction](channel, data, nil)
			}
		}
	}()

	return channel, nil
}

func (d *client) RunTransaction(
	ctx context.Context,
	transaction *stroppy.DriverTransaction,
) error {
	_, err := d.protoClient.RunTransaction(ctx, transaction)

	return err
}

func (d *client) Teardown(ctx context.Context) error {
	_, err := d.protoClient.Teardown(ctx, &emptypb.Empty{})

	return err
}

func ConnectToPlugin( //nolint: ireturn // need from lib
	runConfig *stroppy.RunConfig,
	lg *zap.Logger,
) (Plugin, context.CancelFunc, error) {
	logger.SetLoggerEnv(
		logger.LevelFromProtoConfig(runConfig.GetLogger().GetLogLevel()),
		logger.ModeFromProtoConfig(runConfig.GetLogger().GetLogMode()),
	)

	clientPlugin := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: PluginHandshake,
		Plugins: map[string]plugin.Plugin{
			PluginName: NewSharedPlugin(nil),
		},
		Cmd: exec.Command( //nolint: gosec // allow
			"sh",
			"-c",
			runConfig.GetDriver().GetDriverPluginPath()+" "+
				strings.Join(runConfig.GetDriver().GetDriverPluginArgs(), " "),
		),
		Logger:           NewLogger(lg.Named(driverClientLoggerName)),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Stderr:           os.Stderr,
		SyncStderr:       os.Stderr,
		SyncStdout:       os.Stdout,
	})

	rpcClient, err := clientPlugin.Client()
	if err != nil {
		return nil, clientPlugin.Kill, err
	}

	raw, err := rpcClient.Dispense(PluginName)
	if err != nil {
		return nil, clientPlugin.Kill, err
	}

	return raw.(Plugin), clientPlugin.Kill, nil //nolint: errcheck,forcetypeassert // allow
}
