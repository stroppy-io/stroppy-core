package driver

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/stroppy-io/stroppy-core/pkg/logger"
	stroppy "github.com/stroppy-io/stroppy-core/pkg/proto"
)

type server struct {
	impl Plugin
	*stroppy.UnimplementedDriverPluginServer
}

func newDriverServer(impl Plugin) *server {
	return &server{
		impl:                            impl,
		UnimplementedDriverPluginServer: &stroppy.UnimplementedDriverPluginServer{},
	}
}

func (s server) Initialize(
	ctx context.Context,
	context *stroppy.StepContext,
) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.impl.Initialize(ctx, context)
}

func (s server) BuildQueries(
	ctx context.Context,
	context *stroppy.BuildQueriesContext,
) (*stroppy.DriverQueriesList, error) {
	return s.impl.BuildQueries(ctx, context)
}

func (s server) RunQuery(
	ctx context.Context,
	query *stroppy.DriverQuery,
) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.impl.RunQuery(ctx, query)
}

func (s server) Teardown(
	ctx context.Context,
	_ *emptypb.Empty,
) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.impl.Teardown(ctx)
}

func ServePlugin(impl Plugin) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: PluginHandshake,
		Plugins: map[string]plugin.Plugin{
			PluginName: NewSharedPlugin(impl),
		},
		// A non-nil value here enables gRPC serving for this plugin...
		GRPCServer: plugin.DefaultGRPCServer,
		Logger:     NewLogger(logger.NewFromEnv()),
	})
}
