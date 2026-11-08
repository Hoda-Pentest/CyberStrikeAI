package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

// RegisterBatchTaskMCPTools registers MCP tools related to the batch task queue (requires passing in an AgentHandler with an initialized DB)
func RegisterBatchTaskMCPTools(mcpServer *mcp.Server, h *AgentHandler, logger *zap.Logger) {
	if mcpServer == nil || h == nil || logger == nil {
		return
	}

	reg := func(tool mcp.Tool, fn func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error)) {
		mcpServer.RegisterTool(tool, fn)
	}

	// --- list ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskList,
		Description:      "List batch task queues (concise summary, saves context). Includes queue metadata, subtask id/status/truncated message, and counts per status. For full subtasks (including result/error/conversationId/time, etc.) use batch_task_get(queue_id).\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly mentions viewing/managing batch tasks or task queues. Do not call it on your own when the user has not asked.",
		ShortDescription: "List batch task queues",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status": map[string]interface{}{
					"type":        "string",
					"description": "filter status: all (default), pending, running, paused, completed, cancelled",
					"enum":        []string{"all", "pending", "running", "paused", "completed", "cancelled"},
				},
				"keyword": map[string]interface{}{
					"type":        "string",
					"description": "fuzzy search by queue ID or title",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "page number, starting from 1, default 1",
				},
				"page_size": map[string]interface{}{
					"type":        "integer",
					"description": "items per page, default 20, max 100",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		status := mcpArgString(args, "status")
		if status == "" {
			status = "all"
		}
		keyword := mcpArgString(args, "keyword")
		page := int(mcpArgFloat(args, "page"))
		if page <= 0 {
			page = 1
		}
		pageSize := int(mcpArgFloat(args, "page_size"))
		if pageSize <= 0 {
			pageSize = 20
		}
		if pageSize > 100 {
			pageSize = 100
		}
		offset := (page - 1) * pageSize
		if offset > 100000 {
			offset = 100000
		}
		queues := []*BatchTaskQueue{}
		total := 0
		var err error
		if principal, ok := authctx.PrincipalFromContext(ctx); ok {
			queues, total, err = h.batchTaskManager.ListQueuesForAccess(pageSize, offset, status, keyword, principal.UserID, principal.ScopeFor("tasks:read"))
		} else {
			return batchMCPTextResult("missing authentication identity", true), nil
		}
		if err != nil {
			return batchMCPTextResult(fmt.Sprintf("failed to list queues: %v", err), true), nil
		}
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		slim := make([]batchTaskQueueMCPListItem, 0, len(queues))
		for _, q := range queues {
			if q == nil {
				continue
			}
			slim = append(slim, toBatchTaskQueueMCPListItem(q))
		}
		payload := map[string]interface{}{
			"queues":      slim,
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
		}
		logger.Info("MCP batch_task_list", zap.String("status", status), zap.Int("total", total))
		return batchMCPJSONResult(payload)
	})

	// --- get ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskGet,
		Description:      "Get the details of a single batch task queue by queue_id (including the subtask list, Cron, schedule switch, and the most recent error).\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly mentions viewing/managing batch tasks or task queues. Do not call it on your own when the user has not asked.",
		ShortDescription: "Get batch task queue details",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, ok := h.batchTaskManager.GetBatchQueue(qid)
		if !ok {
			return batchMCPTextResult("the queue does not exist: "+qid, true), nil
		}
		return batchMCPJSONResult(queue)
	})

	// --- create ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskCreate,
		Description: `⚠️ Call constraint: this tool belongs to the "task management" module, and may only be called when the user explicitly requests creating a batch task or task queue. It is forbidden to call it on your own when the user has not mentioned keywords such as "batch task", "task queue", or "scheduled task". If the user just asks you to do something, complete it directly in the current conversation; do not create a task queue on your own initiative.

[Purpose] The in-app "task management / batch task queue": registers multiple mutually independent user instructions as a queue, making it easy to view progress, pause/resume, and schedule reruns in the UI. This is queue data and a scheduling entry, not opening another "sub-agent session" to explore the current problem for you.

[When to use] Call it when the user explicitly wants to batch-queue execution, run the same batch of instructions on a Cron cycle, or needs to align with the task management page. Analysis/coding that requires immediate follow-up questions and strongly depends on the current conversation context should be completed directly in this conversation; do not create a queue for the sake of "delegation".

[Parameters] Choose one of tasks (a string array) or tasks_text (multi-line, one per line); each item is an instruction that the system will execute in queue order. agent_mode: eino_single (Eino ADK single agent, default), deep / plan_execute / supervisor (requires the system to enable multi-agent). Not "splitting the main conversation off to a sub-agent". schedule_mode: manual (default) or cron; cron must fill in cron_expr (5 fields, e.g. "0 */6 * * *").

[Execution] By default it is pending after creation and does not run automatically. execute_now=true runs it immediately after creation; otherwise call batch_task_start later. The next Cron round requires schedule_enabled to be true (via batch_task_schedule_enabled).`,
		ShortDescription: "Task management: create a batch task queue (register multiple instructions, optionally run immediately or by Cron)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "optional queue title, for easier identification in task management",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "role name used by the queue, empty means default",
				},
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "subtask instructions in the queue, each item an independent pending instruction (use either this or tasks_text)",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tasks_text": map[string]interface{}{
					"type":        "string",
					"description": "multi-line text, one subtask instruction per line (use either this or tasks)",
				},
				"agent_mode": map[string]interface{}{
					"type":        "string",
					"description": "execution mode: eino_single (Eino ADK, default), deep/plan_execute/supervisor (Eino orchestration, requires multi-agent enabled)",
					"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
				},
				"schedule_mode": map[string]interface{}{
					"type":        "string",
					"description": "manual (manual/run-after-start only) or cron (triggered by expression)",
					"enum":        []string{"manual", "cron"},
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "required when schedule_mode is cron. Standard 5 fields: minute hour day month weekday, e.g. \"0 */6 * * *\", \"30 2 * * 1-5\"",
				},
				"execute_now": map[string]interface{}{
					"type":        "boolean",
					"description": "whether to start executing the queue immediately after creation, default false (pending, requires batch_task_start)",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "project ID bound to the queue's sub-conversations (optional, uses config.project.default_project_id when not specified)",
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "number of subtasks to execute concurrently, default 1 (serial), max 8. 1-2 is recommended when scanning tools are involved.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		tasks, errMsg := batchMCPTasksFromArgs(args)
		if errMsg != "" {
			return batchMCPTextResult(errMsg, true), nil
		}
		title := mcpArgString(args, "title")
		role := mcpArgString(args, "role")
		agentMode := config.NormalizeAgentMode(mcpArgString(args, "agent_mode"))
		scheduleMode := normalizeBatchQueueScheduleMode(mcpArgString(args, "schedule_mode"))
		cronExpr := strings.TrimSpace(mcpArgString(args, "cron_expr"))
		var nextRunAt *time.Time
		if scheduleMode == "cron" {
			if cronExpr == "" {
				return batchMCPTextResult("cron_expr cannot be empty in Cron schedule mode", true), nil
			}
			sch, err := h.batchCronParser.Parse(cronExpr)
			if err != nil {
				return batchMCPTextResult("invalid Cron expression: "+err.Error(), true), nil
			}
			n := sch.Next(time.Now())
			nextRunAt = &n
		}
		executeNow, ok := mcpArgBool(args, "execute_now")
		if !ok {
			executeNow = false
		}
		projectID := strings.TrimSpace(mcpArgString(args, "project_id"))
		if principal, ok := authctx.PrincipalFromContext(ctx); ok && projectID != "" && principal.ScopeFor("tasks:write") != database.RBACScopeAll {
			if h.db == nil || !h.db.UserCanAccessResource(principal.UserID, principal.ScopeFor("tasks:write"), "project", projectID) {
				return batchMCPTextResult("no permission to access the target project", true), nil
			}
		}
		concurrency := int(mcpArgFloat(args, "concurrency"))
		queue, createErr := h.batchTaskManager.CreateBatchQueue(title, role, agentMode, scheduleMode, cronExpr, projectID, nextRunAt, concurrency, tasks)
		if createErr != nil {
			return batchMCPTextResult("failed to create the queue: "+createErr.Error(), true), nil
		}
		if principal, ok := authctx.PrincipalFromContext(ctx); ok && h.db != nil {
			_ = h.db.SetResourceOwner("batch_task", queue.ID, principal.UserID)
			_ = h.db.AssignResourceToUser(principal.UserID, "batch_task", queue.ID)
		}
		started := false
		if executeNow {
			ok, err := h.startBatchQueueExecution(queue.ID, false)
			if !ok {
				return batchMCPTextResult("the queue does not exist: "+queue.ID, true), nil
			}
			if err != nil {
				return batchMCPTextResult("created successfully but failed to start: "+err.Error(), true), nil
			}
			started = true
			if refreshed, exists := h.batchTaskManager.GetBatchQueue(queue.ID); exists {
				queue = refreshed
			}
		}
		logger.Info("MCP batch_task_create", zap.String("queueId", queue.ID), zap.Int("taskCount", len(tasks)))
		return batchMCPJSONResult(map[string]interface{}{
			"queue_id":    queue.ID,
			"queue":       queue,
			"started":     started,
			"execute_now": executeNow,
			"reminder": func() string {
				if started {
					return "the queue has been created and started immediately."
				}
				return "the queue has been created and is currently pending. To start executing it, call the MCP tool batch_task_start (same queue_id). Cron auto-scheduling requires schedule_enabled to be true, which can be set via batch_task_schedule_enabled."
			}(),
		})
	})

	// --- start ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskStart,
		Description: `Start or continue executing a batch task queue (pending / paused).
Used together with batch_task_create: merely creating a queue does not run it automatically; this tool must be called to start running the subtasks.

⚠️ Call constraint: this tool belongs to the "task management" module, and may only be called when the user explicitly requests starting/continuing batch tasks. Do not call it on your own when the user has not asked.`,
		ShortDescription: "Start/continue a batch task queue (must be called after creation to run)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		ok, err := h.startBatchQueueExecution(qid, false)
		if !ok {
			return batchMCPTextResult("the queue does not exist: "+qid, true), nil
		}
		if err != nil {
			return batchMCPTextResult("failed to start: "+err.Error(), true), nil
		}
		logger.Info("MCP batch_task_start", zap.String("queueId", qid))
		return batchMCPTextResult("start submitted, the queue will begin executing.", false), nil
	})

	// --- rerun (reset + start for completed/cancelled queues) ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskRerun,
		Description:      "Rerun a completed or cancelled batch task queue. It resets all subtask states and then runs another round.\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly requests rerunning batch tasks. Do not call it on your own when the user has not asked.",
		ShortDescription: "Rerun a batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, exists := h.batchTaskManager.GetBatchQueue(qid)
		if !exists {
			return batchMCPTextResult("the queue does not exist: "+qid, true), nil
		}
		if queue.Status != "completed" && queue.Status != "cancelled" {
			return batchMCPTextResult("only completed or cancelled queues can be rerun, current status: "+queue.Status, true), nil
		}
		if !h.batchTaskManager.ResetQueueForRerun(qid) {
			return batchMCPTextResult("failed to reset the queue", true), nil
		}
		ok, err := h.startBatchQueueExecution(qid, false)
		if !ok {
			return batchMCPTextResult("failed to start", true), nil
		}
		if err != nil {
			return batchMCPTextResult("failed to start: "+err.Error(), true), nil
		}
		logger.Info("MCP batch_task_rerun", zap.String("queueId", qid))
		return batchMCPTextResult("the queue has been reset and restarted.", false), nil
	})

	// --- pause ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskPause,
		Description:      "Pause a running batch task queue (the current subtask will be cancelled).\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly requests pausing batch tasks. Do not call it on your own when the user has not asked.",
		ShortDescription: "Pause a batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		if !h.batchTaskManager.PauseQueue(qid) {
			return batchMCPTextResult("cannot pause: the queue does not exist or is not currently running", true), nil
		}
		logger.Info("MCP batch_task_pause", zap.String("queueId", qid))
		return batchMCPTextResult("the queue has been paused.", false), nil
	})

	// --- delete queue ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskDelete,
		Description:      "Delete a batch task queue and its subtask records.\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly requests deleting a batch task queue. Do not call it on your own when the user has not asked.",
		ShortDescription: "Delete a batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		if err := h.batchTaskManager.DeleteQueue(qid); err != nil {
			switch {
			case errors.Is(err, ErrBatchQueueNotFound):
				return batchMCPTextResult("deletion failed: the queue does not exist", true), nil
			case errors.Is(err, ErrBatchQueueExecutorActive):
				return batchMCPTextResult("deletion failed: the queue executor is still running, please try again later", true), nil
			case errors.Is(err, ErrBatchQueueStillRunning):
				return batchMCPTextResult("deletion failed: the queue is running", true), nil
			default:
				return batchMCPTextResult("deletion failed: "+err.Error(), true), nil
			}
		}
		logger.Info("MCP batch_task_delete", zap.String("queueId", qid))
		return batchMCPTextResult("the queue has been deleted.", false), nil
	})

	// --- update metadata (title/role/agentMode) ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskUpdateMetadata,
		Description:      "Modify the title, role, and agent mode of a batch task queue. Can only be modified when the queue is not running.\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly requests modifying batch task queue attributes. Do not call it on your own when the user has not asked.",
		ShortDescription: "Modify the batch task queue title/role/agent mode",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "new title (empty string clears the title)",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "new role name (empty string uses the default role)",
				},
				"agent_mode": map[string]interface{}{
					"type":        "string",
					"description": "agent mode: eino_single, deep, plan_execute, supervisor",
					"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "number of subtasks to execute concurrently, default 1, max 8",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		title := mcpArgString(args, "title")
		role := mcpArgString(args, "role")
		agentMode := mcpArgString(args, "agent_mode")
		var concurrency *int
		if raw, ok := args["concurrency"]; ok && raw != nil {
			v := int(mcpArgFloat(args, "concurrency"))
			concurrency = &v
		}
		if err := h.batchTaskManager.UpdateQueueMetadata(qid, title, role, agentMode, concurrency); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		updated, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_metadata", zap.String("queueId", qid))
		return batchMCPJSONResult(updated)
	})

	// --- update schedule ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskUpdateSchedule,
		Description: `Modify the schedule method and Cron expression of a batch task queue. Can only be modified when the queue is not running.
When schedule_mode is cron, a valid cron_expr must be provided; when manual, the Cron config is cleared.

⚠️ Call constraint: this tool belongs to the "task management" module, and may only be called when the user explicitly requests modifying the batch task schedule config. Do not call it on your own when the user has not asked.`,
		ShortDescription: "Modify the batch task schedule config (Cron expression)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
				"schedule_mode": map[string]interface{}{
					"type":        "string",
					"description": "manual or cron",
					"enum":        []string{"manual", "cron"},
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "Cron expression (required when schedule_mode is cron). Standard 5-field format: minute hour day month weekday, e.g. \"0 */6 * * *\" (every 6 hours), \"30 2 * * 1-5\" (2:30 AM on weekdays)",
				},
			},
			"required": []string{"queue_id", "schedule_mode"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, exists := h.batchTaskManager.GetBatchQueue(qid)
		if !exists {
			return batchMCPTextResult("the queue does not exist: "+qid, true), nil
		}
		if queue.Status == "running" {
			return batchMCPTextResult("the queue is running, cannot modify the schedule config", true), nil
		}
		scheduleMode := normalizeBatchQueueScheduleMode(mcpArgString(args, "schedule_mode"))
		cronExpr := strings.TrimSpace(mcpArgString(args, "cron_expr"))
		var nextRunAt *time.Time
		if scheduleMode == "cron" {
			if cronExpr == "" {
				return batchMCPTextResult("cron_expr cannot be empty in Cron schedule mode", true), nil
			}
			sch, err := h.batchCronParser.Parse(cronExpr)
			if err != nil {
				return batchMCPTextResult("invalid Cron expression: "+err.Error(), true), nil
			}
			n := sch.Next(time.Now())
			nextRunAt = &n
		}
		h.batchTaskManager.UpdateQueueSchedule(qid, scheduleMode, cronExpr, nextRunAt)
		updated, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_schedule", zap.String("queueId", qid), zap.String("scheduleMode", scheduleMode), zap.String("cronExpr", cronExpr))
		return batchMCPJSONResult(updated)
	})

	// --- schedule enabled ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskScheduleEnabled,
		Description: `Set whether to allow Cron to automatically trigger this queue. After turning it off, the Cron expression is still kept, only the scheduled auto-run is stopped; it can still be run manually via "Start".
Only meaningful for queues whose schedule_mode is cron.

⚠️ Call constraint: this tool belongs to the "task management" module, and may only be called when the user explicitly requests toggling batch task auto-scheduling. Do not call it on your own when the user has not asked.`,
		ShortDescription: "Toggle batch task Cron auto-scheduling",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
				"schedule_enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "true allows scheduled triggering, false is manual execution only",
				},
			},
			"required": []string{"queue_id", "schedule_enabled"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		en, ok := mcpArgBool(args, "schedule_enabled")
		if !ok {
			return batchMCPTextResult("schedule_enabled must be a boolean", true), nil
		}
		if _, exists := h.batchTaskManager.GetBatchQueue(qid); !exists {
			return batchMCPTextResult("the queue does not exist", true), nil
		}
		if !h.batchTaskManager.SetScheduleEnabled(qid, en) {
			return batchMCPTextResult("update failed", true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_schedule_enabled", zap.String("queueId", qid), zap.Bool("enabled", en))
		return batchMCPJSONResult(queue)
	})

	// --- add task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskAdd,
		Description:      "Append a subtask to a queue in the pending state.\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly requests adding a subtask to a batch task queue. Do not call it on your own when the user has not asked.",
		ShortDescription: "Add a subtask to a batch queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "task instruction content",
				},
			},
			"required": []string{"queue_id", "message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		msg := strings.TrimSpace(mcpArgString(args, "message"))
		if qid == "" || msg == "" {
			return batchMCPTextResult("queue_id and message cannot be empty", true), nil
		}
		task, err := h.batchTaskManager.AddTaskToQueue(qid, msg)
		if err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_add_task", zap.String("queueId", qid), zap.String("taskId", task.ID))
		return batchMCPJSONResult(map[string]interface{}{"task": task, "queue": queue})
	})

	// --- update task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskUpdate,
		Description:      "Modify the text of subtasks still in pending in the pending queue.\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly requests modifying batch subtask content. Do not call it on your own when the user has not asked.",
		ShortDescription: "Update batch subtask content",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "subtask ID",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "new task instruction",
				},
			},
			"required": []string{"queue_id", "task_id", "message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		tid := mcpArgString(args, "task_id")
		msg := strings.TrimSpace(mcpArgString(args, "message"))
		if qid == "" || tid == "" || msg == "" {
			return batchMCPTextResult("queue_id, task_id, and message cannot be empty", true), nil
		}
		if err := h.batchTaskManager.UpdateTaskMessage(qid, tid, msg); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_task", zap.String("queueId", qid), zap.String("taskId", tid))
		return batchMCPJSONResult(queue)
	})

	// --- remove task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskRemove,
		Description:      "Delete subtasks still in pending from the pending queue.\n\n⚠️ Call constraint: this tool belongs to the \"task management\" module, and may only be called when the user explicitly requests deleting batch subtasks. Do not call it on your own when the user has not asked.",
		ShortDescription: "Delete batch subtasks",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "queue ID",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "subtask ID",
				},
			},
			"required": []string{"queue_id", "task_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		tid := mcpArgString(args, "task_id")
		if qid == "" || tid == "" {
			return batchMCPTextResult("queue_id and task_id cannot be empty", true), nil
		}
		if err := h.batchTaskManager.DeleteTask(qid, tid); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_remove_task", zap.String("queueId", qid), zap.String("taskId", tid))
		return batchMCPJSONResult(queue)
	})

	logger.Debug("batch task MCP tools registered", zap.Int("count", 12))
}

// --- batch_task_list simplified structure (avoids stuffing each subtask's result and other large text blocks into the list context) ---

const mcpBatchListTaskMessageMaxRunes = 160

// batchTaskMCPListSummary the subtask summary in the list (full fields via batch_task_get)
type batchTaskMCPListSummary struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// batchTaskQueueMCPListItem the queue summary in the list
type batchTaskQueueMCPListItem struct {
	ID                    string                    `json:"id"`
	Title                 string                    `json:"title,omitempty"`
	Role                  string                    `json:"role,omitempty"`
	AgentMode             string                    `json:"agentMode"`
	ScheduleMode          string                    `json:"scheduleMode"`
	CronExpr              string                    `json:"cronExpr,omitempty"`
	NextRunAt             *time.Time                `json:"nextRunAt,omitempty"`
	ScheduleEnabled       bool                      `json:"scheduleEnabled"`
	LastScheduleTriggerAt *time.Time                `json:"lastScheduleTriggerAt,omitempty"`
	Status                string                    `json:"status"`
	CreatedAt             time.Time                 `json:"createdAt"`
	StartedAt             *time.Time                `json:"startedAt,omitempty"`
	CompletedAt           *time.Time                `json:"completedAt,omitempty"`
	CurrentIndex          int                       `json:"currentIndex"`
	Concurrency           int                       `json:"concurrency"`
	TaskTotal             int                       `json:"task_total"`
	TaskCounts            map[string]int            `json:"task_counts"`
	Tasks                 []batchTaskMCPListSummary `json:"tasks"`
}

func truncateStringRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	n := 0
	for i := range s {
		if n == maxRunes {
			out := strings.TrimSpace(s[:i])
			if out == "" {
				return "…"
			}
			return out + "…"
		}
		n++
	}
	return s
}

const mcpBatchListMaxTasksPerQueue = 200 // the maximum number of subtask summaries returned per queue in the list

func toBatchTaskQueueMCPListItem(q *BatchTaskQueue) batchTaskQueueMCPListItem {
	counts := map[string]int{
		"pending":   0,
		"running":   0,
		"completed": 0,
		"failed":    0,
		"cancelled": 0,
	}
	tasks := make([]batchTaskMCPListSummary, 0, len(q.Tasks))
	for _, t := range q.Tasks {
		if t == nil {
			continue
		}
		counts[t.Status]++
		// the list view limits the number of subtask summaries; see the full list via batch_task_get
		if len(tasks) < mcpBatchListMaxTasksPerQueue {
			tasks = append(tasks, batchTaskMCPListSummary{
				ID:      t.ID,
				Status:  t.Status,
				Message: truncateStringRunes(t.Message, mcpBatchListTaskMessageMaxRunes),
			})
		}
	}
	return batchTaskQueueMCPListItem{
		ID:                    q.ID,
		Title:                 q.Title,
		Role:                  q.Role,
		AgentMode:             q.AgentMode,
		ScheduleMode:          q.ScheduleMode,
		CronExpr:              q.CronExpr,
		NextRunAt:             q.NextRunAt,
		ScheduleEnabled:       q.ScheduleEnabled,
		LastScheduleTriggerAt: q.LastScheduleTriggerAt,
		Status:                q.Status,
		CreatedAt:             q.CreatedAt,
		StartedAt:             q.StartedAt,
		CompletedAt:           q.CompletedAt,
		CurrentIndex:          q.CurrentIndex,
		Concurrency:           q.Concurrency,
		TaskTotal:             len(tasks),
		TaskCounts:            counts,
		Tasks:                 tasks,
	}
}

func batchMCPTextResult(text string, isErr bool) *mcp.ToolResult {
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: text}},
		IsError: isErr,
	}
}

func batchMCPJSONResult(v interface{}) (*mcp.ToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return batchMCPTextResult(fmt.Sprintf("JSON encoding failed: %v", err), true), nil
	}
	return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: string(b)}}}, nil
}

func batchMCPTasksFromArgs(args map[string]interface{}) ([]string, string) {
	if raw, ok := args["tasks"]; ok && raw != nil {
		switch t := raw.(type) {
		case []interface{}:
			out := make([]string, 0, len(t))
			for _, x := range t {
				if s, ok := x.(string); ok {
					if tr := strings.TrimSpace(s); tr != "" {
						out = append(out, tr)
					}
				}
			}
			if len(out) > 0 {
				return out, ""
			}
		}
	}
	if txt := mcpArgString(args, "tasks_text"); txt != "" {
		lines := strings.Split(txt, "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if tr := strings.TrimSpace(line); tr != "" {
				out = append(out, tr)
			}
		}
		if len(out) > 0 {
			return out, ""
		}
	}
	return nil, "you must provide tasks (a string array) or tasks_text (multi-line text, one task per line)"
}

func mcpArgString(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(t, 'f', -1, 64))
	case json.Number:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func mcpArgFloat(args map[string]interface{}, key string) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

func mcpArgBool(args map[string]interface{}, key string) (val bool, ok bool) {
	v, exists := args[key]
	if !exists {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		if s == "true" || s == "1" || s == "yes" {
			return true, true
		}
		if s == "false" || s == "0" || s == "no" {
			return false, true
		}
	case float64:
		return t != 0, true
	}
	return false, false
}
