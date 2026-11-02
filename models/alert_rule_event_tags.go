package models

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/ccfos/nightingale/v6/pkg/tplx"
)

// RenderEventTags combines sample labels with rendered rule tags for engine and
// test events. It returns a new map and preserves inline template error values.
func (ar *AlertRule) RenderEventTags(labels map[string]string) map[string]string {
	tagsMap := make(map[string]string, len(labels)+len(ar.AppendTagsJSON)+1)
	for k, v := range labels {
		tagsMap[k] = v
	}
	event := &AlertCurEvent{TagsMap: tagsMap}

	tags := make([]string, 0, len(ar.AppendTagsJSON)+1)
	tags = append(tags, ar.AppendTagsJSON...)
	tags = append(tags, "rulename="+ar.Name)
	for _, tag := range tags {
		pair := strings.SplitN(tag, "=", 2)
		if len(pair) != 2 || pair[0] == "" {
			continue
		}
		text := "{{$labels := .TagsMap}}{{$value := .TriggerValue}}" + pair[1]
		tpl, err := template.New(fmt.Sprint(ar.Id)).Funcs(template.FuncMap(tplx.TemplateFuncMap)).Parse(text)
		if err != nil {
			tagsMap[pair[0]] = fmt.Sprintf("parse tag value failed, err:%s", err)
			continue
		}
		var body bytes.Buffer
		if err := tpl.Execute(&body, event); err != nil {
			tagsMap[pair[0]] = fmt.Sprintf("parse tag value failed, err:%s", err)
			continue
		}
		tagsMap[pair[0]] = body.String()
	}
	return tagsMap
}
