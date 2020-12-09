local jaeger_mixin = import 'github.com/grafana/jsonnet-libs/jaeger-agent-mixin/jaeger.libsonnet';
local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet',
      container = k.core.v1.container,
      containerPort = k.core.v1.containerPort,
      deployment = k.apps.v1.deployment;

{
  _images+:: {
    grpc_cortex_gw: 'jdbgrafana/grpc-cortex-gw:latest',
  },

  _config+:: {
    replicas: 3,
    args: {
      'cortex.endpoint': 'dns:///distributor.svc:9095',
      'server.listen-address': ':8080',
    },
  },

  grpc_cortex_gw_container::
    container.new('grpc-cortex-gw', $._images.grpc_cortex_gw) +
    container.withPorts([
      containerPort.newNamed(name='http', containerPort=8080),
    ]) +
    container.withArgsMixin(k.util.mapToFlags($._config.args)) +
    k.util.resourcesRequests('1', '512Mi') +
    jaeger_mixin,

  grpc_cortex_gw_deployment:
    deployment.new('grpc-cortex-gw', $._config.replicas, [$.grpc_cortex_gw_container]) +
    k.util.antiAffinity,

  grpc_cortex_gw_service:
    k.util.serviceFor($.grpc_cortex_gw_deployment),
}
