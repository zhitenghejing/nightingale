package aiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/ccfos/nightingale/v6/pkg/modeloutput"

	"github.com/toolkits/pkg/logger"
)

// 本文件实现 agent 的唯一执行体：原生 function-calling 工具循环（ReAct 文本
// 协议与无工具的 Direct 模式均已删，不存在其他执行路径）。
// 工具经 tools 参数下发、调用经 tool_calls 解析，结构化 transcript 持久化。
//
// 流式通道约定（与路由层抽卡/A2A 的既有契约严格一致；字段独占路由：
// 思考与正文互斥，绝不双发）：
//   - 推理增量（reasoning_content / 思考块）→ StreamTypeThinking（thinking 面板）。
//   - 模型正文增量 → StreamTypeContent 实时流，直接落 content 通道。中间轮的
//     pre-tool-call 评论同样进 content（轮间补段落分隔）。最终答案因此已逐
//     token 下发——executeNativeWithDone 的 Done chunk 据 contentStreamed 打标，
//     路由层只把 Done.Content 当作解析/持久化用的权威正文（= 最终轮正文，
//     不含中间轮评论），不再二次推流，无重复。
//   - 工具调用/结果 emit StreamTypeToolCall / StreamTypeToolResult
//     （Metadata["tool"]）→ 抽卡与 A2A 据此渲染。

// executeNative 工具循环的执行入口（Run 的流式/非流式两条路径都经由此函数）。
func (a *Agent) executeNative(ctx context.Context, req *AgentRequest, rc *runCtx) *AgentResponse {
	userMessage, err := a.buildUserMessage(req)
	if err != nil {
		return &AgentResponse{Error: fmt.Sprintf("failed to build user message: %v", err)}
	}

	systemPrompt := a.buildNativeSystemPrompt(rc)

	// 历史投影：canonical transcript 不变，喂模型的是截断/收窗后的投影
	// （见 context_manager.go）。
	messages := []ChatMessage{{Role: "system", Content: systemPrompt}}
	if len(req.History) > 0 {
		messages = append(messages, projectHistory(req.History, a.cfg.HistoryBudgetBytes)...)
	}
	messages = append(messages, ChatMessage{Role: "user", Content: userMessage})

	requestID := ""
	if req.Metadata != nil {
		requestID = req.Metadata["request_id"]
	}

	logger.Infof("[Agent] native starting: messages=%d, system_len=%d, user_len=%d, history=%d, tools=%d, streaming=%v",
		len(messages), len(systemPrompt), len(userMessage), len(req.History), len(rc.tools), req.StreamChan != nil)

	return a.runNativeLoop(ctx, req, messages, buildNativeToolDefs(rc.tools), &ToolLoopConfig{
		GuardEvidenceProgress: a.cfg.GuardEvidenceProgress,
		MaxIterations:         a.maxIterationsForSkills(rc),
		MaxToolCalls:          a.cfg.MaxToolCalls,
		FinishOnToolBudget:    a.cfg.FinishOnToolBudget,
		TimeoutMessage:        "agent execution timeout",
		LogPrefix:             "AI Agent(native)",
		Tools:                 rc.tools,
		StreamChan:            req.StreamChan,
		RequestID:             requestID,
		ExtractPartialResult:  true,
		EmitTranscript:        true, // 顶层 chat：本轮 transcript 持久化以供下一轮回放
	})
}

// runNativeLoop 原生 function-calling 循环：调 LLM（带 tools）→ 无 tool_calls
// 即最终答案；有则逐个执行、回灌结构化 tool 结果轮，并 emit 流式事件 +
// 结构化 transcript。
func (a *Agent) runNativeLoop(ctx context.Context, req *AgentRequest, messages []ChatMessage, toolDefs []llm.ToolDefinition, config *ToolLoopConfig) *AgentResponse {
	resp := &AgentResponse{Steps: []ToolStep{}}
	streaming := config.StreamChan != nil
	dedup := newTurnWriteDeduper() // 写类工具幂等：整个循环（跨迭代）内同名同参只执行一次
	readonlyFailures := map[string]int{}
	protocolRepaired := false
	progress := newEvidenceProgress()
	lastSourceRound := false

	for iteration := 0; iteration < config.MaxIterations; iteration++ {
		select {
		case <-ctx.Done():
			resp.Error = config.TimeoutMessage
			resp.Iterations = iteration
			return resp
		default:
		}

		callCtx := ctx
		cancelCall := func() {}
		if config.GuardEvidenceProgress && lastSourceRound && progress.hasEvidence() {
			if deadline, ok := ctx.Deadline(); ok {
				cutoff := deadline.Add(-120 * time.Second)
				if time.Until(cutoff) <= 0 {
					return a.finishEvidenceCollection(ctx, req, messages, config, resp, iteration)
				}
				callCtx, cancelCall = context.WithDeadline(ctx, cutoff)
			}
		}
		llmStart := time.Now()
		content, calls, thinking, err := a.callLLMNative(callCtx, messages, toolDefs, config.StreamChan, config.RequestID, req.Params["lang"])
		llmDuration := time.Since(llmStart).Milliseconds()
		planningExpired := callCtx.Err() != nil && ctx.Err() == nil
		cancelCall()
		if planningExpired && config.GuardEvidenceProgress && progress.hasEvidence() {
			return a.finishEvidenceCollection(ctx, req, messages, config, resp, iteration)
		}
		if err != nil {
			emitTranscript(config, ChatMessage{Role: "assistant", DurationMS: llmDuration, FailureClass: "model_call_failed"})
			resp.Error = fmt.Sprintf("LLM call failed at iteration %d: %v", iteration, err)
			resp.Iterations = iteration
			return resp
		}

		logger.Debugf("%s iteration %d: content_len=%d, tool_calls=%d", config.LogPrefix, iteration, len(content), len(calls))

		// 无工具调用 = 最终答案（原生协议的天然完成信号）。
		if len(calls) == 0 {
			if modeloutput.ProtocolOnly(content) {
				// Preserve the real malformed response, but keep the corrective
				// instruction out of canonical history and evidence boundaries.
				emitTranscript(config, ChatMessage{Role: "assistant", Content: content, ThinkingBlocks: thinking})
				if !protocolRepaired && iteration+1 < config.MaxIterations && ctx.Err() == nil && (config.MaxToolCalls <= 0 || len(resp.Steps) < config.MaxToolCalls) {
					protocolRepaired = true
					resp.ProtocolRecoveries++
					messages = append(messages, ChatMessage{Role: "assistant", Content: content, ThinkingBlocks: thinking}, ChatMessage{Role: "user", Content: "上一轮仅返回协议标记或空白，未形成可见答案。请按原生tools结构继续原问题所需的工具调用，或依据已取得证据回答；证据不足时说明具体缺口。遵守当前工具权限和审批，不重复已执行的写操作，不要将工具标记当作正文。"})
					continue
				}
				resp.Error = "模型未形成可核对的可见答复。"
				resp.Iterations = iteration + 1
				return resp
			}
			resp.Content = content
			resp.contentStreamed = streaming // 正文已逐 token 走 StreamTypeContent，Done 不再推流
			resp.Iterations = iteration + 1
			resp.Success = true
			return resp
		}

		// 中间轮评论（与 tool_calls 同轮的正文）已实时进 content 通道；补一个
		// 段落分隔，避免与后续轮正文/最终答案在 markdown 里粘连成一段。
		if streaming && strings.TrimSpace(content) != "" {
			config.StreamChan <- &StreamChunk{
				Type:      StreamTypeContent,
				Delta:     "\n\n",
				RequestID: config.RequestID,
				Timestamp: time.Now().UnixMilli(),
			}
		}

		// Gemini 等不回 call id 的端点：合成确定性 id，保证 transcript 回放时
		// tool_call_id 配对完整（OpenAI/Claude 回放都要求成对）。
		for i := range calls {
			if calls[i].ID == "" {
				calls[i].ID = fmt.Sprintf("call_%d_%d", iteration, i)
			}
		}

		// ThinkingBlocks 随 assistant 轮进入上下文与 transcript：Anthropic 系开启
		// thinking 后，续轮回填这些块（带签名）是 API 硬性要求。
		asstTurn := ChatMessage{Role: "assistant", Content: content, ToolCalls: calls, ThinkingBlocks: thinking, DurationMS: llmDuration}
		progress.beginRound()
		messages = append(messages, asstTurn)
		turn := []ChatMessage{asstTurn}

		for callIndex, tc := range calls {
			if config.MaxToolCalls > 0 && len(resp.Steps) >= config.MaxToolCalls {
				if config.FinishOnToolBudget {
					// Pair every remaining tool_use without executing it, then finalize without tools.
					for _, pending := range calls[callIndex:] {
						m := ChatMessage{Role: llm.RoleTool, ToolCallID: pending.ID, ToolName: pending.Name, Content: "查询预算已用尽，此调用未执行。"}
						messages = append(messages, m)
						turn = append(turn, m)
					}
					emitTranscript(config, turn...)
					return a.finishNativeBudget(ctx, req, messages, config, resp, iteration+1)
				}
				resp.Error = fmt.Sprintf("reached maximum tool calls (%d)", config.MaxToolCalls)
				resp.Iterations = iteration + 1
				return resp
			}
			step := ToolStep{Thought: content, Action: tc.Name, ActionInput: tc.Arguments}
			toolStart := time.Now()

			if streaming {
				config.StreamChan <- &StreamChunk{
					Type:      StreamTypeToolCall,
					Content:   tc.Name,
					Metadata:  map[string]interface{}{"input": tc.Arguments},
					RequestID: config.RequestID,
					Timestamp: time.Now().UnixMilli(),
				}
			}

			var observation string
			failureArgs := tc.Arguments
			var parsedArgs map[string]interface{}
			if json.Unmarshal([]byte(tc.Arguments), &parsedArgs) == nil {
				if canonical, err := json.Marshal(parsedArgs); err == nil {
					failureArgs = string(canonical)
				}
			}
			failureKey := tc.Name + "\x00" + failureArgs
			if !isWriteTool(tc.Name) && readonlyFailures[failureKey] >= 2 {
				observation = "Error: 相同只读查询已连续失败两次；请按缺失字段修正参数，或说明该项未完成。此调用未执行。"
			} else if cached, hit := dedup.lookup(tc.Name, tc.Arguments); hit {
				// 同轮同名同参的写工具重复调用：复用首次结果，不重复落库。
				observation = cached + "\n\n(本轮已用相同参数执行过该写操作，以上为首次执行结果，未重复执行)"
			} else {
				var interrupt *ToolInterrupt
				observation, interrupt = a.executeToolI(ctx, tc.Name, tc.Arguments, req, config.Tools)
				if interrupt != nil {
					// 人在环中断：确认文案即本轮答复；被中断的调用不产 ToolResult/
					// transcript（重放所需状态在 Pending 里，无需进入对话历史）。
					// 同轮先行工具可能已执行（写操作已落库）：把已完成的调用配对
					// 持久化，否则下一轮模型对已发生的副作用失忆、可能重复执行。
					// assistant 轮的 ToolCalls 必须裁剪到只保留有结果的调用——带上
					// 被中断/未执行的调用会形成孤儿 tool_use，回放链路没有配对修复
					// （assembleTurnHistory 纯拼接），provider 会整单拒绝。
					if len(turn) > 1 {
						executed := turn[1:]
						asst := turn[0]
						asst.ToolCalls = asst.ToolCalls[:len(executed)] // 按序执行，前 N 个即已完成的
						emitTranscript(config, append([]ChatMessage{asst}, executed...)...)
					}
					emitInterrupt(config, interrupt, tc.Name)
					resp.Content = interrupt.Prompt
					resp.Iterations = iteration + 1
					resp.Success = true
					return resp
				}
				dedup.record(tc.Name, tc.Arguments, observation)
				if !isWriteTool(tc.Name) && strings.HasPrefix(observation, "Error:") {
					readonlyFailures[failureKey]++
				}
			}

			// 工具渐进披露：load_skill 成功后把该
			// 技能声明的 builtin_tools 注入工具表并重建 toolDefs，下一迭代原生
			// tools 参数即包含它们；观测尾部附注新工具名，模型当轮即知。
			if tc.Name == "load_skill" && !strings.HasPrefix(observation, "Error:") {
				if name := skillNameFromLoadArgs(tc.Arguments); name != "" {
					var added []string
					config.Tools, added = a.injectSkillTools(config.Tools, name)
					if len(added) > 0 {
						toolDefs = buildNativeToolDefs(config.Tools)
						observation += "\n\n(已随技能启用工具: " + strings.Join(added, ", ") + ")"
					}
				}
			}
			progress.observe(tc.Name, tc.Arguments, observation)
			toolDuration := time.Since(toolStart).Milliseconds()
			step.Observation = observation
			resp.Steps = append(resp.Steps, step)

			if streaming {
				config.StreamChan <- &StreamChunk{
					Type:      StreamTypeToolResult,
					Content:   observation,
					Metadata:  map[string]interface{}{"tool": tc.Name, "arguments": tc.Arguments},
					RequestID: config.RequestID,
					Timestamp: time.Now().UnixMilli(),
				}
			}

			toolTurn := ChatMessage{Role: llm.RoleTool, ToolCallID: tc.ID, ToolName: tc.Name, Content: observation, DurationMS: toolDuration}
			// messages 收的是投影副本：本轮观测在这里就按 LiveObservationCapBytes
			// 收口，否则一次超大工具结果会原样留在 messages 里，被剩余每次迭代
			// 重复送进请求（跨轮的 projectHistory 此时还够不着它）。turn 进
			// canonical transcript，永远收原文。
			messages = append(messages, capObservation(toolTurn, LiveObservationCapBytes))
			turn = append(turn, toolTurn)
		}

		// 结构化 transcript：assistant(tool_calls) + tool 结果轮成组持久化，
		// 工具产物（如 proposal_id）下一轮直接以原生形态回放。
		emitTranscript(config, turn...)
		lastSourceRound = progress.endRound()
		if config.GuardEvidenceProgress && lastSourceRound && progress.stalled() {
			return a.finishEvidenceCollection(ctx, req, messages, config, resp, iteration+1)
		}
		if config.GuardEvidenceProgress && lastSourceRound && progress.warn() {
			messages = append(messages, ChatMessage{Role: "user", Content: "上一轮取证没有增加新的来源、记录或单元格。逐项核对原问题要求的来源类别与字段：导入备注不等于已读取原台账，不同资料类别不能互相替代。先核对matched_records/read_documents；所需事实齐全就直接回答，仍缺来源或字段时只补查缺项，勿换关键词重复检索已有记录。参数类型报错先修正再继续，不要把未执行的调用说成已检索。"})
		}
		if config.FinishOnToolBudget && config.MaxToolCalls > 0 && len(resp.Steps) >= config.MaxToolCalls {
			return a.finishNativeBudget(ctx, req, messages, config, resp, iteration+1)
		}
	}

	if config.FinishOnToolBudget && len(resp.Steps) > 0 {
		return a.finishNativeBudget(ctx, req, messages, config, resp, config.MaxIterations)
	}
	resp.Error = fmt.Sprintf("reached maximum iterations (%d)", config.MaxIterations)
	resp.Iterations = config.MaxIterations
	if config.ExtractPartialResult && len(resp.Steps) > 0 {
		last := resp.Steps[len(resp.Steps)-1]
		resp.Content = fmt.Sprintf("Analysis incomplete (max iterations reached). Last thought: %s", last.Thought)
	}
	return resp
}

// callLLMNative 以原生 tools 参数调用 LLM 并聚合单次调用的结果（正文、
// 工具调用、思考块）。
//
// 流式时按字段独占路由（杜绝"思考面板 ≡ 回答"的双发）：推理增量走
// StreamTypeThinking，正文增量走 StreamTypeContent，互斥。
// 工具调用由各 provider 在流内聚合完整后整块抛出（OpenAI 按 index 归槽、
// Claude 在 content_block_stop 收尾），这里直接累积即可；
// Anthropic 系的完整思考块（含签名）在块收尾时整块到达，累积后随 assistant 轮回填。
func (a *Agent) callLLMNative(ctx context.Context, messages []ChatMessage, toolDefs []llm.ToolDefinition, streamChan chan *StreamChunk, requestID, lang string) (string, []llm.ToolCall, []llm.ThinkingBlock, error) {
	if err := a.checkLLMClient(); err != nil {
		return "", nil, nil, err
	}

	llmReq := buildLLMRequest(messages)
	llmReq.Tools = toolDefs

	if streamChan == nil {
		genResp, err := a.llmClient.Generate(ctx, llmReq)
		if err != nil {
			return "", nil, nil, err
		}
		content, err := applyFinishReason(genResp.Content, genResp.ToolCalls, genResp.FinishReason, lang, nil)
		return content, genResp.ToolCalls, genResp.ThinkingBlocks, err
	}

	stream, err := a.llmClient.GenerateStream(ctx, llmReq)
	if err != nil {
		logger.Errorf("[Agent] GenerateStream(native) failed provider=%s: %v", a.llmClient.Name(), err)
		return "", nil, nil, fmt.Errorf("LLM stream error: %w", err)
	}

	var content strings.Builder
	var calls []llm.ToolCall
	var thinking []llm.ThinkingBlock
	var finishReason string
	emit := func(chunkType, delta string) {
		streamChan <- &StreamChunk{
			Type:      chunkType,
			Delta:     delta,
			RequestID: requestID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	for chunk := range stream {
		if chunk.Error != nil {
			go drainStream(stream)
			return content.String(), calls, thinking, fmt.Errorf("stream error: %w", chunk.Error)
		}
		if chunk.Reasoning != "" {
			emit(StreamTypeThinking, chunk.Reasoning)
		}
		if chunk.ThinkingBlock != nil {
			thinking = append(thinking, *chunk.ThinkingBlock)
		}
		if chunk.Content != "" {
			content.WriteString(chunk.Content)
			emit(StreamTypeContent, chunk.Content)
		}
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
		calls = append(calls, chunk.ToolCalls...)
		if chunk.Done {
			break
		}
	}

	finalContent, err := applyFinishReason(content.String(), calls, finishReason, lang,
		func(note string) { emit(StreamTypeContent, note) })
	return finalContent, calls, thinking, err
}

// applyFinishReason 把 provider 上报的异常结束原因转成显式信号：拦截类
// （SAFETY/content_filter/refusal）判错——正文不可信，不能伪装成完整回答；
// 截断类（max_tokens/length）在正文尾部追加可见提示，半截答案仍有价值。
// 有 tool_calls 时不干预：截断的调用参数本就靠工具执行报错喂回模型重试
// （见 openai.go finish 的注释），整轮报错反而打断该机制。
// lang 取 req.Params["lang"]（router 注入的 UI 语言），截断提示按其选取 zh/en。
func applyFinishReason(content string, calls []llm.ToolCall, reason, lang string, emitNote func(string)) (string, error) {
	if len(calls) > 0 {
		return content, nil
	}
	switch llm.ClassifyFinish(reason) {
	case llm.FinishBlocked:
		return content, fmt.Errorf("LLM output blocked by provider (finish_reason=%s)", reason)
	case llm.FinishTruncated:
		note := LangText(lang,
			"\n\n（输出达到长度上限，回答被截断）",
			"\n\n(The output hit the length limit and the answer was truncated.)")
		if emitNote != nil {
			emitNote(note)
		}
		return content + note, nil
	}
	return content, nil
}

// buildNativeToolDefs 把 AgentTool 的扁平参数表编译成 JSON-Schema 形态的
// llm.ToolDefinition，供原生 tools 参数下发。
func buildNativeToolDefs(tools []AgentTool) []llm.ToolDefinition {
	defs := make([]llm.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		props := map[string]interface{}{}
		var required []string
		for _, p := range t.Parameters {
			property := map[string]interface{}{
				"type":        normalizeJSONSchemaType(p.Type),
				"description": p.Description,
			}
			if normalizeJSONSchemaType(p.Type) == "array" {
				items := map[string]interface{}{}
				if p.ItemType != "" {
					items["type"] = normalizeJSONSchemaType(p.ItemType)
				}
				property["items"] = items
			}
			if len(p.Enum) > 0 {
				property["enum"] = p.Enum
			}
			props[p.Name] = property
			if p.Required {
				required = append(required, p.Name)
			}
		}
		params := map[string]interface{}{"type": "object", "properties": props}
		if len(required) > 0 {
			params["required"] = required
		}
		defs = append(defs, llm.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}
	return defs
}

// normalizeJSONSchemaType 把工具定义里随手写的类型名归一到 JSON-Schema 类型，
// 未知类型按 string 兜底（provider 对非法类型会整单拒绝，宁可宽松）。
func normalizeJSONSchemaType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "int", "integer", "int64":
		return "integer"
	case "bool", "boolean":
		return "boolean"
	case "float", "double", "number":
		return "number"
	case "object", "map":
		return "object"
	case "array", "list":
		return "array"
	default:
		return "string"
	}
}

// maxIterationsForSkills 取全局默认与所有激活 skill 声明的 MaxIterations 的最大值
// （skill 可在 frontmatter 为多步工作流抬高上限）。
func (a *Agent) maxIterationsForSkills(rc *runCtx) int {
	maxIter := a.cfg.MaxIterations
	for _, sk := range rc.skills {
		if sk == nil || sk.Metadata == nil {
			continue
		}
		if sk.Metadata.MaxIterations > maxIter {
			maxIter = sk.Metadata.MaxIterations
			logger.Infof("[Agent] skill %q raised MaxIterations to %d", sk.Metadata.Name, maxIter)
		}
	}
	return maxIter
}

// emitInterrupt 把工具的人在环中断交给路由层（持久化 Pending 并结束本轮）。
func emitInterrupt(config *ToolLoopConfig, ti *ToolInterrupt, toolName string) {
	if config.StreamChan == nil {
		return
	}
	config.StreamChan <- &StreamChunk{
		Type:    StreamTypeInterrupt,
		Content: ti.Prompt,
		Metadata: map[string]interface{}{
			"kind":        ti.Kind,
			"tool":        toolName,
			"resume_args": ti.ResumeArgs,
			// form 仅 input 类非空：form_select 载荷，路由层渲染成与 preflight
			// 同契约的表单 response。
			"form": ti.Form,
			// 仅 approval 类：确认并重放成功后，是否把工具结果回接 agent 续跑。
			"resume_after_confirm": ti.ResumeAfterConfirm,
		},
		RequestID: config.RequestID,
		Timestamp: time.Now().UnixMilli(),
	}
}

// emitTranscript 在流式且开启 EmitTranscript 时，把本轮新追加的规范消息经 StreamChan
// 交给路由层持久化。非流式或关闭时为 no-op。
func emitTranscript(config *ToolLoopConfig, msgs ...ChatMessage) {
	if config.StreamChan == nil || !config.EmitTranscript || len(msgs) == 0 {
		return
	}
	config.StreamChan <- &StreamChunk{
		Type:       StreamTypeTranscript,
		Transcript: msgs,
		RequestID:  config.RequestID,
		Timestamp:  time.Now().UnixMilli(),
	}
}

// executeNativeWithDone 执行工具循环并在流式模式下发送 done/error chunk
// 用于流式模式的顶层调用
func (a *Agent) executeNativeWithDone(ctx context.Context, req *AgentRequest, rc *runCtx) {
	streamChan := req.StreamChan
	requestID := ""
	if req.Metadata != nil {
		requestID = req.Metadata["request_id"]
	}

	resp := a.executeNative(ctx, req, rc)

	// 纯失败（LLM 错误/超时，无任何可展示产出）才走 Error chunk。带部分产出的
	// max-iteration 截断（ExtractPartialResult 拼好的 "Analysis incomplete..."）
	// 落到下面的 Done 分支：路由层把这类非流式正文当唯一拷贝推流并持久化
	// （见 router 消费侧 "max-iteration partials" 注释），用户能看到已有分析
	// 而非一句超时错误。
	if resp.Error != "" && !resp.Success && resp.Content == "" {
		streamChan <- &StreamChunk{
			Type:      StreamTypeError,
			Error:     resp.Error,
			Done:      true,
			RequestID: requestID,
			Timestamp: time.Now().UnixMilli(),
		}
		return
	}

	done := &StreamChunk{
		Type:      StreamTypeDone,
		Content:   resp.Content,
		Done:      true,
		RequestID: requestID,
		Timestamp: time.Now().UnixMilli(),
	}
	if resp.contentStreamed {
		// 流式路径：正文已逐 token 走 StreamTypeContent 下发。打标让路由层
		// 把 Done.Content 仅当作解析/持久化用的权威正文（= 最终轮正文，不含中间轮
		// 评论），不再二次推流——否则回答整段重复。
		done.Metadata = map[string]interface{}{"content_streamed": true}
	}
	if resp.ProtocolRecoveries > 0 {
		if done.Metadata == nil {
			done.Metadata = map[string]interface{}{}
		}
		done.Metadata["protocol_recoveries"] = resp.ProtocolRecoveries
	}
	streamChan <- done
}

// Only explicitly opted-in read workflows use this path. It makes one final
// inference with no tools and never executes an unsolicited call from that reply.
func (a *Agent) finishNativeBudget(ctx context.Context, req *AgentRequest, messages []ChatMessage, config *ToolLoopConfig, resp *AgentResponse, iterations int) *AgentResponse {
	return a.finishNativeSummary(ctx, req, messages, config, resp, iterations, "本轮查询额度已用尽，请停止工具调用。", "\n\n本轮查询已达到上限，未完成的检查仍待核实。")
}

func (a *Agent) finishEvidenceCollection(ctx context.Context, req *AgentRequest, messages []ChatMessage, config *ToolLoopConfig, resp *AgentResponse, iterations int) *AgentResponse {
	return a.finishNativeSummary(ctx, req, messages, config, resp, iterations, "取证未增加新记录或已到预留作答窗口，请停止工具调用。", "")
}
func (a *Agent) finishNativeSummary(ctx context.Context, req *AgentRequest, messages []ChatMessage, config *ToolLoopConfig, resp *AgentResponse, iterations int, reason, suffix string) *AgentResponse {
	if ctx.Err() != nil {
		resp.Error = config.TimeoutMessage
		return resp
	}
	messages = append(messages, ChatMessage{Role: llm.RoleUser, Content: reason + "直接回答用户最新问题，只依据已经取得的证据；明确未完成的查询和不能判断的事项，不复述整个历史故障，不把未知状态说成正常。"})
	content, calls, _, err := a.callLLMNative(ctx, messages, nil, config.StreamChan, config.RequestID, req.Params["lang"])
	if err != nil {
		resp.Error = fmt.Sprintf("final evidence summary failed: %v", err)
		return resp
	}
	if len(calls) > 0 {
		resp.Error = "final evidence summary requested unavailable tools"
		return resp
	}
	if modeloutput.ProtocolOnly(content) {
		resp.Error = "模型未形成可核对的可见答复。"
		return resp
	}
	resp.Content = content + suffix
	resp.Success = true
	resp.Iterations = iterations + 1
	resp.contentStreamed = false
	return resp
}
