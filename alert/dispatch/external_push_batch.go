package dispatch

import (
	"sync"

	"github.com/ccfos/nightingale/v6/alert/sender"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/externalpush"
	"github.com/toolkits/pkg/logger"
)

// A batch spans the original event and its matching subscriptions. Existing
// pipelines run once; old providers keep their existing asynchronous path.
type externalDispatchBatch struct {
	wg     sync.WaitGroup
	mu     sync.Mutex
	routes []externalpush.Submission
}

func (b *externalDispatchBatch) add(route externalpush.Submission) {
	b.mu.Lock()
	b.routes = append(b.routes, route)
	b.mu.Unlock()
}

func (b *externalDispatchBatch) flush(c *ctx.Context) {
	b.wg.Wait()
	if len(b.routes) == 0 {
		return
	}
	ids, err := externalpush.RegisterDispatch(c, externalpush.DispatchSubmission{Routes: b.routes})
	if err != nil {
		for _, route := range b.routes {
			sender.NotifyRecord(c, []*models.AlertCurEvent{route.Event}, route.NotifyRuleID, "external_push", "external-push", "dispatch route registration failed; inspect task ledger", err)
		}
		return
	}
	logger.Infof("external push dispatch tasks registered: task_ids=%v dispatch_id=%s; delivery results are in dedicated ledger", ids, b.routes[0].Event.DispatchID)
}
