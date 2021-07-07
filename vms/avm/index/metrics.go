// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import (
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	numTxsIndexed prometheus.Histogram
}

func (m *metrics) initialize(namespace string, registerer prometheus.Registerer) error {
	m.numTxsIndexed = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "txs_indexed",
		Help:      "Number of transactions indexed",
	})
	return registerer.Register(m.numTxsIndexed)
}
