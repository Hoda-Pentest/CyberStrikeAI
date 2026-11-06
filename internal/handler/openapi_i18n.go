package handler

// apiDocI18n provides x-i18n-* extension keys for the OpenAPI document, used by the frontend apiDocs internationalization.
// The frontend translates them via apiDocs.tags.* / apiDocs.summary.* / apiDocs.response.*.

var apiDocI18nTagToKey = map[string]string{
	"Authentication": "auth", "Conversation Management": "conversationManagement", "Conversation Interaction": "conversationInteraction",
	"Batch Tasks": "batchTasks", "Vulnerability Management": "vulnerabilityManagement",
	"Role Management": "roleManagement", "Skills Management": "skillsManagement", "Monitoring": "monitoring",
	"Configuration Management": "configManagement", "External MCP Management": "externalMCPManagement", "Attack Chain": "attackChain",
	"Knowledge Base": "knowledgeBase", "MCP": "mcp",
	"FOFA Reconnaissance": "fofaRecon", "Terminal": "terminal", "WebShell Management": "webshellManagement",
	"Conversation Attachments": "chatUploads", "Robot Integration": "robotIntegration", "Multi-Agent Markdown": "markdownAgents",
	"Project Management": "projectManagement", "Asset Management": "assetManagement",
}

var apiDocI18nSummaryToKey = map[string]string{
	"User login": "login", "User logout": "logout", "Change password": "changePassword", "Validate token": "validateToken",
	"Create conversation": "createConversation", "List conversations": "listConversations", "View conversation details": "getConversationDetail",
	"Update conversation": "updateConversation", "Delete conversation": "deleteConversation", "Get conversation result": "getConversationResult",
	"Send message and get AI reply (non-streaming)": "sendMessageNonStream", "Send message and get AI reply (streaming)": "sendMessageStream",
	"Cancel task": "cancelTask", "List running tasks": "listRunningTasks", "List completed tasks": "listCompletedTasks",
	"Create batch task queue": "createBatchQueue", "List batch task queues": "listBatchQueues", "Get batch task queue": "getBatchQueue",
	"Delete batch task queue": "deleteBatchQueue", "Start batch task queue": "startBatchQueue", "Pause batch task queue": "pauseBatchQueue",
	"Add task to queue": "addTaskToQueue", "SQL injection scan": "sqlInjectionScan", "Port scan": "portScan",
	"Update batch task": "updateBatchTask", "Delete batch task": "deleteBatchTask",
	"List vulnerabilities": "listVulnerabilities", "Create vulnerability": "createVulnerability", "Get vulnerability statistics": "getVulnerabilityStats",
	"Get vulnerability": "getVulnerability", "Update vulnerability": "updateVulnerability", "Delete vulnerability": "deleteVulnerability",
	"List roles": "listRoles", "Create role": "createRole", "Get role": "getRole", "Update role": "updateRole", "Delete role": "deleteRole",
	"Get available skills list": "getAvailableSkills", "List skills": "listSkills", "Create skill": "createSkill",
	"Get skill statistics": "getSkillStats", "Clear skill statistics": "clearSkillStats", "Get skill": "getSkill",
	"Update skill": "updateSkill", "Delete skill": "deleteSkill", "Get bound roles": "getBoundRoles",
	"Get monitoring information": "getMonitorInfo", "Get execution records": "getExecutionRecords", "Delete execution record": "deleteExecutionRecord",
	"Batch delete execution records": "batchDeleteExecutionRecords", "Get statistics": "getStats",
	"Get configuration": "getConfig", "Update configuration": "updateConfig", "Get tool configuration": "getToolConfig", "Apply configuration": "applyConfig",
	"List external MCPs": "listExternalMCP", "Get external MCP statistics": "getExternalMCPStats", "Get external MCP": "getExternalMCP",
	"Add or update external MCP": "addOrUpdateExternalMCP", "stdio mode configuration": "stdioModeConfig", "SSE mode configuration": "sseModeConfig",
	"Delete external MCP": "deleteExternalMCP", "Start external MCP": "startExternalMCP", "Stop external MCP": "stopExternalMCP",
	"Get attack chain": "getAttackChain", "Regenerate attack chain": "regenerateAttackChain",
	"Pin conversation": "pinConversation",
	"Get categories":   "getCategories", "List knowledge items": "listKnowledgeItems", "Create knowledge item": "createKnowledgeItem",
	"Get knowledge item": "getKnowledgeItem", "Update knowledge item": "updateKnowledgeItem", "Delete knowledge item": "deleteKnowledgeItem",
	"Get index status": "getIndexStatus", "Build index": "startKnowledgeIndex", "Scan knowledge base": "scanKnowledgeBase",
	"Search knowledge base": "searchKnowledgeBase", "Basic search": "basicSearch", "Search by risk type": "searchByRiskType",
	"Get retrieval logs": "getRetrievalLogs", "Delete retrieval log": "deleteRetrievalLog",
	"MCP endpoint": "mcpEndpoint", "List all tools": "listAllTools", "Invoke tool": "invokeTool", "Initialize connection": "initConnection",
	"Success response": "successResponse", "Error response": "errorResponse",
	// Newly added missing endpoints
	"Delete conversation turn": "deleteConversationTurn", "Get message process details": "getMessageProcessDetails",
	"Rerun batch task queue": "rerunBatchQueue", "Update queue metadata": "updateBatchQueueMetadata",
	"Update queue schedule configuration": "updateBatchQueueSchedule", "Toggle cron auto-scheduling": "setBatchQueueScheduleEnabled",
	"FOFA search": "fofaSearch", "Parse natural language into FOFA syntax": "fofaParse",
	"Test OpenAI API connection": "testOpenAI",
	"Run terminal command":       "terminalRun", "Run terminal command (streaming)": "terminalRunStream", "WebSocket terminal": "terminalWS",
	"List WebShell connections": "listWebshellConnections", "Create WebShell connection": "createWebshellConnection",
	"Update WebShell connection": "updateWebshellConnection", "Delete WebShell connection": "deleteWebshellConnection",
	"Get connection state": "getWebshellConnectionState", "Save connection state": "saveWebshellConnectionState",
	"Get AI conversation history": "getWebshellAIHistory", "List AI conversations": "listWebshellAIConversations",
	"Execute WebShell command": "webshellExec", "WebShell file operation": "webshellFileOp",
	"List attachments": "listChatUploads", "Export attachments": "exportChatUploads", "Upload attachment": "uploadChatFile", "Delete attachment": "deleteChatUpload",
	"Download attachment": "downloadChatUpload", "Get attachment text content": "getChatUploadContent",
	"Write attachment text content": "putChatUploadContent", "Create attachment directory": "mkdirChatUpload", "Rename attachment": "renameChatUpload",
	"WeCom callback verification": "wecomCallbackVerify", "WeCom message callback": "wecomCallbackMessage",
	"DingTalk message callback": "dingtalkCallback", "Lark message callback": "larkCallback", "Test robot message handling": "testRobot",
	"List Markdown agents": "listMarkdownAgents", "Create Markdown agent": "createMarkdownAgent",
	"Get Markdown agent details": "getMarkdownAgent", "Update Markdown agent": "updateMarkdownAgent", "Delete Markdown agent": "deleteMarkdownAgent",
	"List skill package files": "listSkillPackageFiles", "Get skill package file content": "getSkillPackageFile", "Write skill package file": "putSkillPackageFile",
	"Batch get tool names":          "batchGetToolNames",
	"Get knowledge base statistics": "getKnowledgeStats",
	"List projects":                 "listProjects", "Create project": "createProject", "Get project": "getProject",
	"Update project": "updateProject", "Delete project": "deleteProject",
	"Batch import assets":      "importAssets",
	"List facts or get by key": "listProjectFacts", "Create/update fact": "upsertProjectFact",
	"Get project fact attack-path graph": "getProjectFactGraph", "List all fact edges of a project": "listProjectFactEdges",
	"Add fact edge": "createProjectFactEdge", "Delete fact edge": "deleteProjectFactEdge",
	"Promote conversation attack chain to project fact graph": "promoteAttackChainToProject",
}

var apiDocI18nResponseDescToKey = map[string]string{
	"Retrieved successfully": "getSuccess", "Unauthorized": "unauthorized", "Unauthorized, a valid token is required": "unauthorizedToken",
	"Created successfully": "createSuccess", "Invalid request parameters": "badRequest", "Conversation not found": "conversationNotFound",
	"Conversation or result not found": "conversationOrResultNotFound", "Invalid request parameters (e.g., task is empty)": "badRequestTaskEmpty",
	"Invalid request parameters (e.g., malformed configuration, missing required fields)": "badRequestConfig",
	"Invalid request parameters (e.g., query is empty)":                                   "badRequestQueryEmpty", "Method not allowed (only POST requests are supported)": "methodNotAllowed",
	"Login successful": "loginSuccess", "Incorrect password": "invalidPassword", "Logout successful": "logoutSuccess",
	"Password changed successfully": "passwordChanged", "Token is valid": "tokenValid", "Token is invalid or expired": "tokenInvalid",
	"Conversation created successfully": "conversationCreated", "Internal server error": "internalError", "Updated successfully": "updateSuccess",
	"Deleted successfully": "deleteSuccess", "Queue not found": "queueNotFound", "Started successfully": "startSuccess",
	"Paused successfully": "pauseSuccess", "Added successfully": "addSuccess",
	"Task not found":           "taskNotFound",
	"Cancel request submitted": "cancelSubmitted", "No running task found": "noRunningTask",
	"Message sent successfully, AI reply returned": "messageSent", "Streaming response (Server-Sent Events)": "streamResponse",
	// Newly added missing endpoint responses
	"Invalid parameters or deletion failed": "badRequestOrDeleteFailed",
	"Parameter error":                       "paramError", "Only completed or cancelled queues can be rerun": "onlyCompletedOrCancelledCanRerun",
	"Invalid parameters or the queue is running": "badRequestOrQueueRunning", "Set successfully": "setSuccess",
	"Search successful": "searchSuccess", "Parsed successfully": "parseSuccess", "Test result": "testResult",
	"Execution completed": "executionDone", "SSE event stream": "sseEventStream", "WebSocket connection established": "wsEstablished",
	"File download": "fileDownload", "File not found": "fileNotFound", "Written successfully": "writeSuccess",
	"Renamed successfully": "renameSuccess", "Verification successful, decrypted echostr returned": "wecomVerifySuccess",
	"Processed successfully": "processSuccess", "Agent not found": "agentNotFound", "Saved successfully": "saveSuccess",
	"Operation result": "operationResult", "Execution result": "executionResult", "Connection not found": "connectionNotFound",
	"Project list": "projectList", "Project details": "projectDetail",
	"Fact list or a single fact (may include link_counts / outgoing_links)": "projectFactList",
	"Success": "success", "nodes + edges": "factGraphNodesEdges",
	"Edge list": "edgeList", "Edge created": "edgeCreated",
	"Promotion result (facts/edges/graph)": "promoteAttackChainResult",
	"Import completed":                     "assetImportCompleted", "Count or asset field validation failed": "assetImportValidationFailed",
	"Missing asset:write permission or no access to the specified project": "assetImportForbidden",
	"Import transaction failed": "assetImportTransactionFailed",
}

// enrichSpecWithI18nKeys writes x-i18n-tags and x-i18n-summary on every operation in the spec,
// and x-i18n-description on every response, so the frontend can internationalize by key.
func enrichSpecWithI18nKeys(spec map[string]interface{}) {
	paths, _ := spec["paths"].(map[string]interface{})
	if paths == nil {
		return
	}
	for _, pathItem := range paths {
		pm, _ := pathItem.(map[string]interface{})
		if pm == nil {
			continue
		}
		for _, method := range []string{"get", "post", "put", "delete", "patch"} {
			opVal, ok := pm[method]
			if !ok {
				continue
			}
			op, _ := opVal.(map[string]interface{})
			if op == nil {
				continue
			}
			// x-i18n-tags: i18n key array corresponding one-to-one to tags (tags in the spec is []string)
			switch tags := op["tags"].(type) {
			case []string:
				if len(tags) > 0 {
					keys := make([]string, 0, len(tags))
					for _, s := range tags {
						if k := apiDocI18nTagToKey[s]; k != "" {
							keys = append(keys, k)
						} else {
							keys = append(keys, s)
						}
					}
					op["x-i18n-tags"] = keys
				}
			case []interface{}:
				if len(tags) > 0 {
					keys := make([]interface{}, 0, len(tags))
					for _, t := range tags {
						if s, ok := t.(string); ok {
							if k := apiDocI18nTagToKey[s]; k != "" {
								keys = append(keys, k)
							} else {
								keys = append(keys, s)
							}
						}
					}
					if len(keys) > 0 {
						op["x-i18n-tags"] = keys
					}
				}
			}
			// x-i18n-summary
			if summary, _ := op["summary"].(string); summary != "" {
				if k := apiDocI18nSummaryToKey[summary]; k != "" {
					op["x-i18n-summary"] = k
				}
			}
			// responses -> each status -> x-i18n-description
			if respMap, _ := op["responses"].(map[string]interface{}); respMap != nil {
				for _, rv := range respMap {
					if r, _ := rv.(map[string]interface{}); r != nil {
						if desc, _ := r["description"].(string); desc != "" {
							if k := apiDocI18nResponseDescToKey[desc]; k != "" {
								r["x-i18n-description"] = k
							}
						}
					}
				}
			}
		}
	}
}
