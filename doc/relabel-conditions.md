# Writer relabel 条件

HTTP 和 Kafka writer 的 `WriteRelabels` 可配置 `If`，只对匹配条件的序列执行当前
规则。不匹配时保留标签并继续下一条规则；每条条件读取前一条规则处理后的标签。

`If` 使用 Prometheus 标签选择器语法，可省略最外层大括号。多个条件以逗号连接，
全部满足才执行；支持 `=`、`!=`、`=~`、`!~`。不存在的标签按空字符串匹配。
精确匹配与正则匹配有区别：`label="x|y"` 匹配字面值 `x|y`，
`label=~"x|y"` 匹配 `x` 或 `y`，正则匹配整个标签值。

```toml
[[Pushgw.Writers.WriteRelabels]]
If = '{env="prod",job=~"api|worker"}'
Action = "replace"
TargetLabel = "team"
Replacement = "operations"
```

`If` 为空时无条件执行。配置在 writer 启动前解析，非法选择器或正则会阻止启动；
直接调用 `writer.Process` 的程序也会检查条件，非法条件不执行当前规则。
原来的导出字段 `IfRegex` 保留兼容，但不参与判断，条件由 `If` 解析。
