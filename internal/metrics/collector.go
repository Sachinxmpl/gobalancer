package metrics

import "github.com/prometheus/client_golang/prometheus"

type StateSource interface {
	BackendAddrs() []string
	Conns(addr string) int64
	Up(addr string) int64
}

type stateCollector struct {
	src             StateSource
	activeConnsDesc *prometheus.Desc
	upDesc          *prometheus.Desc
}

func NewStateCollector(src StateSource) prometheus.Collector {
	return &stateCollector{
		src: src,

		activeConnsDesc: prometheus.NewDesc(
			"gobalancer_active_connections",
			"Current active connections per backend",
			[]string{"backend"},
			nil,
		),

		upDesc: prometheus.NewDesc(
			"gobalancer_backend_up",
			"Backend health state (1=up, 0=down)",
			[]string{"backend"},
			nil,
		),
	}
}

func (c *stateCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.activeConnsDesc
	ch <- c.upDesc
}

func (c *stateCollector) Collect(ch chan<- prometheus.Metric) {
	for _, addr := range c.src.BackendAddrs() {

		ch <- prometheus.MustNewConstMetric(
			c.activeConnsDesc,
			prometheus.GaugeValue,
			float64(c.src.Conns(addr)),
			addr,
		)

		ch <- prometheus.MustNewConstMetric(
			c.upDesc,
			prometheus.GaugeValue,
			float64(c.src.Up(addr)),
			addr,
		)
	}
}
