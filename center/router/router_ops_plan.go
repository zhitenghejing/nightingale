package router

import (
	"github.com/ccfos/nightingale/v6/pkg/ops"
	"github.com/gin-gonic/gin"
)

func (rt *Router) opsPlanPropose(c *gin.Context) {
	var in ops.PlanInput
	if !opsBind(c, &in) {
		return
	}
	p, err := rt.Ops.ProposePlan(opsUser(c), opsID(c, "id"), in)
	opsResult(c, p, err)
}
func (rt *Router) opsPlanDecide(c *gin.Context) {
	var in struct {
		Revision int64  `json:"revision"`
		Accept   bool   `json:"accept"`
		Note     string `json:"note"`
	}
	if !opsBind(c, &in) {
		return
	}
	err := rt.Ops.DecidePlan(opsUser(c), opsID(c, "id"), opsID(c, "plan"), in.Revision, in.Accept, in.Note)
	opsResult(c, nil, err)
}
