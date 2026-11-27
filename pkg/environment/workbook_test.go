package environment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"testing"
)

func TestExcelMentionsRemainCandidatesWithoutAssetWrites(t *testing.T) {
	s := fixture(t)
	document := doc(t, s)
	x := excelize.NewFile()
	defer x.Close()
	require.NoError(t, x.SetCellStr("Sheet1", "A1", "实施报告提及对象"))
	require.NoError(t, x.SetCellStr("Sheet1", "A2", "sim-app-a"))
	require.NoError(t, x.SetCellStr("Sheet1", "A3", "SIM-SN-001"))
	buffer, e := x.WriteToBuffer()
	require.NoError(t, e)
	book, e := workbook.Read(context.Background(), bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	require.NoError(t, e)
	md, _, e := book.Markdown()
	require.NoError(t, e)
	sum := sha256.Sum256([]byte(md))
	require.NoError(t, s.DB.Model(&models.LibraryVersion{}).Where("id=1").Updates(map[string]interface{}{"markdown": md, "digest": fmt.Sprintf("%x", sum)}).Error)
	discovered, e := s.Discover(1, document.Ref)
	require.NoError(t, e)
	require.NotEmpty(t, discovered.Candidates)
	for _, c := range discovered.Candidates {
		require.Equal(t, "candidate", c.Status)
		require.Equal(t, "describes", c.Kind)
	}
	var targets, assets, confirmed int64
	require.NoError(t, s.DB.Model(&models.Target{}).Count(&targets).Error)
	require.NoError(t, s.DB.Model(&cmdb.Asset{}).Count(&assets).Error)
	require.NoError(t, s.DB.Model(&models.EnvironmentRelation{}).Where("status=?", "confirmed").Count(&confirmed).Error)
	require.EqualValues(t, 3, targets)
	require.EqualValues(t, 1, assets)
	require.Zero(t, confirmed)
	require.NoError(t, s.DB.Model(&models.ArchiveDocument{}).Where("id=1").Update("current_version_id", 2).Error)
	_, e = s.Discover(1, document.Ref)
	require.Error(t, e)
}
