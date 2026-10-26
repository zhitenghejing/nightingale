package archives

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
)

func TestArchiveInformationPrintForm(t *testing.T) {
	if os.Getenv("N9E_TEST_ARCHIVE_RENDERER") != "1" {
		t.Skip("set N9E_TEST_ARCHIVE_RENDERER=1 with Node 24, LibreOffice and Poppler")
	}
	s := newTestService(t)
	s.ConfigureExports(os.Getenv("N9E_ARCHIVE_NODE"), "", 120)
	for _, tc := range []struct {
		category, heading, expected, excluded string
		metadata                              Metadata
	}{
		{"training", "培训服务档案表", "培训结束标记", "巡检设备范围", Metadata{Department: "基础设施组", TrainingStatus: "normal", StartTime: "2026-10-24T09:00:00+08:00", Participants: "测试学员甲、乙、丙", TrainingContent: strings.Repeat("分页检查：长内容需要完整保留，并允许表格跨页继续。\n", 80) + "培训结束标记", Scope: []string{"不相关设备"}}},
		{"inspection", "设备巡检档案表", "服务器、网络", "培训部门", Metadata{InspectionResult: "abnormal", Scope: []string{"服务器", "网络"}, Summary: "更换跳线后复查", Department: "不相关部门"}},
		{"incident", "故障处理档案表", "Ⅲ级", "巡检结果", Metadata{Level: "III", AffectedBusiness: "测试订单服务", MainEngineer: "赵工", InspectionResult: "abnormal"}},
		{"other", "业务文档归档表", "值班交接说明", "培训状态", Metadata{CustomType: "操作指引", Summary: "值班交接说明", TrainingStatus: "normal"}},
	} {
		t.Run(tc.category, func(t *testing.T) {
			work := t.TempDir()
			path := filepath.Join(work, "information.pdf")
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			snapshot := Snapshot{Record: models.ArchiveRecord{ID: 18, Category: tc.category, Title: "打印排版验收", State: "archived", Revision: 9, CreateName: "Admin", CreateAt: s.now(), UpdateAt: s.now(), Metadata: encode(tc.metadata)}}
			if err := s.renderInformation(ctx, work, path, snapshot, s.exportRuntime()); err != nil {
				t.Fatal(err)
			}
			if directory := os.Getenv("N9E_ARCHIVE_PRINT_QA_DIR"); directory != "" {
				if err := os.WriteFile(filepath.Join(directory, tc.category+".pdf"), mustRead(t, path), 0600); err != nil {
					t.Fatal(err)
				}
			}
			text, err := exec.Command("pdftotext", "-layout", path, "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			compact := strings.Join(strings.Fields(string(text)), "")
			for _, expected := range []string{tc.heading, tc.expected, "档案主题", "打印排版验收", "档案版本v9", "留档资料清单", "暂无留档资料", "创建账号：Admin", "第1页/共"} {
				if !strings.Contains(compact, expected) {
					t.Fatalf("missing %q in PDF: %s", expected, text)
				}
			}
			if strings.Contains(compact, tc.excluded) || strings.Contains(compact, "[]") || strings.Contains(compact, "不相关") {
				t.Fatalf("unrelated/serialized fields leaked: %s", text)
			}
			if tc.category == "training" && bytes.Count(text, []byte{'\f'}) < 2 {
				t.Fatal("long sample did not exercise pagination")
			}
		})
	}
}
