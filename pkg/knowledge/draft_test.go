package knowledge

import (
	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestGeneratedDraftAcceptsThinkingWrappedJSONWithoutStoringReasoning(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(append(models.KnowledgeTables(), &models.User{}, &models.BusiGroup{})...))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "admin", Roles: "Admin"}).Error)
	require.NoError(t, db.Create(&models.BusiGroup{Id: 1, Name: "test"}).Error)
	s := &Store{DB: db}
	draft, err := s.GeneratedDraft(1, 1, nil, "<think>INTERNAL_DO_NOT_PUBLISH</think>\n```json\n{\"title\":\"Draft\",\"spec\":{\"summary\":\"Observed issue\"}}\n```")
	require.NoError(t, err)
	require.NotContains(t, string(draft.Draft), "INTERNAL_DO_NOT_PUBLISH")
	require.Zero(t, draft.CurrentVersionID)
}
