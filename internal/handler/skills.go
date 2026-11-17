package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/skillpackage"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// SkillsHandler is the Skills handler (disk + Agent spec; loaded at runtime by the Pi skill middleware)
type SkillsHandler struct {
	config     *config.Config
	configPath string
	logger     *zap.Logger
	db         *database.DB // database connection (legacy statistics; MCP list/read has been removed)
	audit      *audit.Service
}

// SetAudit wires platform audit logging.
func (h *SkillsHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewSkillsHandler creates a new Skills handler
func NewSkillsHandler(cfg *config.Config, configPath string, logger *zap.Logger) *SkillsHandler {
	return &SkillsHandler{
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

func (h *SkillsHandler) skillsRootAbs() string {
	skillsDir := h.config.SkillsDir
	if skillsDir == "" {
		skillsDir = "skills"
	}
	configDir := filepath.Dir(h.configPath)
	if !filepath.IsAbs(skillsDir) {
		skillsDir = filepath.Join(configDir, skillsDir)
	}
	return skillsDir
}

// SetDB sets the database connection (for fetching call statistics)
func (h *SkillsHandler) SetDB(db *database.DB) {
	h.db = db
}

// GetSkills gets the list of all skills (supports pagination and search)
func (h *SkillsHandler) GetSkills(c *gin.Context) {
	allSummaries, err := skillpackage.ListSkillSummaries(h.skillsRootAbs())
	if err != nil {
		h.logger.Error("failed to get the skills list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	searchKeyword := strings.TrimSpace(c.Query("search"))

	allSkillsInfo := make([]map[string]interface{}, 0, len(allSummaries))
	for _, s := range allSummaries {
		skillInfo := map[string]interface{}{
			"id":           s.ID,
			"name":         s.Name,
			"dir_name":     s.DirName,
			"description":  s.Description,
			"version":      s.Version,
			"path":         s.Path,
			"tags":         s.Tags,
			"triggers":     s.Triggers,
			"script_count": s.ScriptCount,
			"file_count":   s.FileCount,
			"progressive":  s.Progressive,
			"file_size":    s.FileSize,
			"mod_time":     s.ModTime,
		}
		allSkillsInfo = append(allSkillsInfo, skillInfo)
	}

	filteredSkillsInfo := allSkillsInfo
	if searchKeyword != "" {
		keywordLower := strings.ToLower(searchKeyword)
		filteredSkillsInfo = make([]map[string]interface{}, 0)
		for _, skillInfo := range allSkillsInfo {
			id := strings.ToLower(fmt.Sprintf("%v", skillInfo["id"]))
			name := strings.ToLower(fmt.Sprintf("%v", skillInfo["name"]))
			description := strings.ToLower(fmt.Sprintf("%v", skillInfo["description"]))
			path := strings.ToLower(fmt.Sprintf("%v", skillInfo["path"]))
			version := strings.ToLower(fmt.Sprintf("%v", skillInfo["version"]))
			tagsJoined := ""
			if tags, ok := skillInfo["tags"].([]string); ok {
				tagsJoined = strings.ToLower(strings.Join(tags, " "))
			}
			trigJoined := ""
			if tr, ok := skillInfo["triggers"].([]string); ok {
				trigJoined = strings.ToLower(strings.Join(tr, " "))
			}
			if strings.Contains(id, keywordLower) ||
				strings.Contains(name, keywordLower) ||
				strings.Contains(description, keywordLower) ||
				strings.Contains(path, keywordLower) ||
				strings.Contains(version, keywordLower) ||
				strings.Contains(tagsJoined, keywordLower) ||
				strings.Contains(trigJoined, keywordLower) {
				filteredSkillsInfo = append(filteredSkillsInfo, skillInfo)
			}
		}
	}

	// pagination parameters
	limit := 20 // default 20 per page
	offset := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := parseInt(limitStr); err == nil && parsed > 0 {
			// allow a larger limit for search scenarios, but set a reasonable upper bound (10000)
			if parsed <= 10000 {
				limit = parsed
			} else {
				limit = 10000
			}
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsed, err := parseInt(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// compute the pagination range
	total := len(filteredSkillsInfo)
	start := offset
	end := offset + limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	// get the current page's skill list
	var paginatedSkillsInfo []map[string]interface{}
	if start < end {
		paginatedSkillsInfo = filteredSkillsInfo[start:end]
	} else {
		paginatedSkillsInfo = []map[string]interface{}{}
	}

	c.JSON(http.StatusOK, gin.H{
		"skills": paginatedSkillsInfo,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetSkill gets the details of a single skill
func (h *SkillsHandler) GetSkill(c *gin.Context) {
	skillName := c.Param("name")
	if skillName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the skill name cannot be empty"})
		return
	}

	resPath := strings.TrimSpace(c.Query("resource_path"))
	if resPath == "" {
		resPath = strings.TrimSpace(c.Query("skill_script_path"))
	}
	if resPath != "" {
		content, err := skillpackage.ReadScriptText(h.skillsRootAbs(), skillName, resPath, 0)
		if err != nil {
			h.logger.Warn("failed to read the skill resource", zap.String("skill", skillName), zap.String("path", resPath), zap.Error(err))
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"skill": map[string]interface{}{
				"id": skillName,
			},
			"resource": map[string]interface{}{
				"path":    resPath,
				"content": content,
			},
		})
		return
	}

	depthStr := strings.ToLower(strings.TrimSpace(c.DefaultQuery("depth", "full")))
	section := strings.TrimSpace(c.Query("section"))
	opt := skillpackage.LoadOptions{Section: section}
	switch depthStr {
	case "summary":
		opt.Depth = "summary"
	case "full", "":
		opt.Depth = "full"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "depth only supports summary or full"})
		return
	}

	skill, err := skillpackage.LoadSkill(h.skillsRootAbs(), skillName, opt)
	if err != nil {
		h.logger.Warn("failed to load skill", zap.String("skill", skillName), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "the skill does not exist: " + err.Error()})
		return
	}

	skillPath := skill.Path
	skillFile := filepath.Join(skillPath, "SKILL.md")

	fileInfo, _ := os.Stat(skillFile)
	var fileSize int64
	var modTime string
	if fileInfo != nil {
		fileSize = fileInfo.Size()
		modTime = fileInfo.ModTime().Format("2006-01-02 15:04:05")
	}

	c.JSON(http.StatusOK, gin.H{
		"skill": map[string]interface{}{
			"id":            skill.DirName,
			"name":          skill.Name,
			"description":   skill.Description,
			"content":       skill.Content,
			"path":          skill.Path,
			"version":       skill.Version,
			"tags":          skill.Tags,
			"scripts":       skill.Scripts,
			"sections":      skill.Sections,
			"package_files": skill.PackageFiles,
			"file_size":     fileSize,
			"mod_time":      modTime,
			"depth":         depthStr,
			"section":       section,
		},
	})
}

// ListSkillPackageFiles lists all files in a skill directory (Agent Skills layout).
func (h *SkillsHandler) ListSkillPackageFiles(c *gin.Context) {
	skillID := c.Param("name")
	files, err := skillpackage.ListPackageFiles(h.skillsRootAbs(), skillID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// GetSkillPackageFile returns one file by relative path (?path=).
func (h *SkillsHandler) GetSkillPackageFile(c *gin.Context) {
	skillID := c.Param("name")
	rel := strings.TrimSpace(c.Query("path"))
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query path is required"})
		return
	}
	b, err := skillpackage.ReadPackageFile(h.skillsRootAbs(), skillID, rel, 0)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"path": rel, "content": string(b)})
}

// PutSkillPackageFile writes a file inside the skill package.
func (h *SkillsHandler) PutSkillPackageFile(c *gin.Context) {
	skillID := c.Param("name")
	var req struct {
		Path    string `json:"path" binding:"required"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameter: " + err.Error()})
		return
	}
	if req.Path == "SKILL.md" {
		if err := skillpackage.ValidateSkillMDPackage([]byte(req.Content), skillID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if err := skillpackage.WritePackageFile(h.skillsRootAbs(), skillID, req.Path, []byte(req.Content)); err != nil {
		h.logger.Error("failed to write the skill file", zap.String("skill", skillID), zap.String("path", req.Path), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "saved", "path": req.Path})
}

// GetSkillBoundRoles gets the list of roles bound to the specified skill
func (h *SkillsHandler) GetSkillBoundRoles(c *gin.Context) {
	skillName := c.Param("name")
	if skillName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the skill name cannot be empty"})
		return
	}

	boundRoles := h.getRolesBoundToSkill(skillName)
	c.JSON(http.StatusOK, gin.H{
		"skill":       skillName,
		"bound_roles": boundRoles,
		"bound_count": len(boundRoles),
	})
}

// getRolesBoundToSkill reserved: roles no longer configure skill bindings, always returns an empty list.
func (h *SkillsHandler) getRolesBoundToSkill(skillName string) []string {
	_ = skillName
	return nil
}

// CreateSkill creates a new skill (standard Agent Skills: generates SKILL.md + YAML front matter)
func (h *SkillsHandler) CreateSkill(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description" binding:"required"`
		Content     string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameter: " + err.Error()})
		return
	}

	if !isValidSkillName(req.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the skill directory name must be lowercase letters, digits, and hyphens (consistent with the Agent Skills name)"})
		return
	}

	manifest := &skillpackage.SkillManifest{
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
	}
	skillMD, err := skillpackage.BuildSkillMD(manifest, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := skillpackage.ValidateSkillMDPackage(skillMD, req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	skillDir := filepath.Join(h.skillsRootAbs(), req.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		h.logger.Error("failed to create the skill directory", zap.String("skill", req.Name), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create the skill directory: " + err.Error()})
		return
	}

	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the skill already exists"})
		return
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), skillMD, 0644); err != nil {
		h.logger.Error("failed to create SKILL.md", zap.String("skill", req.Name), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create SKILL.md: " + err.Error()})
		return
	}

	h.logger.Info("skill created successfully", zap.String("skill", req.Name))
	if h.audit != nil {
		h.audit.RecordOK(c, "skill", "create", "create Skill", "skill", req.Name, nil)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "skill created",
		"skill": map[string]interface{}{
			"name": req.Name,
			"path": skillDir,
		},
	})
}

// UpdateSkill updates SKILL.md (keeps fields in the front matter other than description; optionally overrides description)
func (h *SkillsHandler) UpdateSkill(c *gin.Context) {
	skillName := c.Param("name")
	if skillName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the skill name cannot be empty"})
		return
	}

	var req struct {
		Description string `json:"description"`
		Content     string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameter: " + err.Error()})
		return
	}

	mdPath := filepath.Join(h.skillsRootAbs(), skillName, "SKILL.md")
	raw, err := os.ReadFile(mdPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "the skill does not exist: " + err.Error()})
		return
	}
	m, _, err := skillpackage.ParseSkillMD(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Description != "" {
		m.Description = strings.TrimSpace(req.Description)
	}
	skillMD, err := skillpackage.BuildSkillMD(m, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := skillpackage.ValidateSkillMDPackage(skillMD, skillName); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	skillDir := filepath.Join(h.skillsRootAbs(), skillName)

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), skillMD, 0644); err != nil {
		h.logger.Error("failed to update SKILL.md", zap.String("skill", skillName), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update SKILL.md: " + err.Error()})
		return
	}

	h.logger.Info("skill updated successfully", zap.String("skill", skillName))
	if h.audit != nil {
		h.audit.RecordOK(c, "skill", "update", "update Skill", "skill", skillName, nil)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "skill updated",
	})
}

// DeleteSkill deletes a skill
func (h *SkillsHandler) DeleteSkill(c *gin.Context) {
	skillName := c.Param("name")
	if skillName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the skill name cannot be empty"})
		return
	}

	// check whether any role is bound to this skill; if so, automatically remove the binding
	affectedRoles := h.removeSkillFromRoles(skillName)
	if len(affectedRoles) > 0 {
		h.logger.Info("remove skill binding from role",
			zap.String("skill", skillName),
			zap.Strings("roles", affectedRoles))
	}

	skillDir := filepath.Join(h.skillsRootAbs(), skillName)
	if err := os.RemoveAll(skillDir); err != nil {
		h.logger.Error("failed to delete the skill", zap.String("skill", skillName), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete the skill: " + err.Error()})
		return
	}
	responseMsg := "skill deleted"
	if len(affectedRoles) > 0 {
		responseMsg = fmt.Sprintf("skill deleted, and its binding was automatically removed from %d roles: %s",
			len(affectedRoles), strings.Join(affectedRoles, ", "))
	}

	h.logger.Info("skill deleted successfully", zap.String("skill", skillName))
	if h.audit != nil {
		h.audit.RecordOK(c, "skill", "delete", "delete Skill", "skill", skillName, map[string]interface{}{
			"affected_roles": affectedRoles,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"message":        responseMsg,
		"affected_roles": affectedRoles,
	})
}

// GetSkillStats gets skills call statistics
func (h *SkillsHandler) GetSkillStats(c *gin.Context) {
	skillList, err := skillpackage.ListSkillDirNames(h.skillsRootAbs())
	if err != nil {
		h.logger.Error("failed to get the skills list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	skillsDir := h.skillsRootAbs()

	// load call statistics from the database
	var skillStatsMap map[string]*database.SkillStats
	if h.db != nil {
		dbStats, err := h.db.LoadSkillStats()
		if err != nil {
			h.logger.Warn("failed to load Skills statistics from the database", zap.Error(err))
			skillStatsMap = make(map[string]*database.SkillStats)
		} else {
			skillStatsMap = dbStats
		}
	} else {
		skillStatsMap = make(map[string]*database.SkillStats)
	}

	// build the statistics (including all skills, even those with no call records)
	statsList := make([]map[string]interface{}, 0, len(skillList))
	totalCalls := 0
	totalSuccess := 0
	totalFailed := 0

	for _, skillName := range skillList {
		stat, exists := skillStatsMap[skillName]
		if !exists {
			stat = &database.SkillStats{
				SkillName:    skillName,
				TotalCalls:   0,
				SuccessCalls: 0,
				FailedCalls:  0,
			}
		}

		totalCalls += stat.TotalCalls
		totalSuccess += stat.SuccessCalls
		totalFailed += stat.FailedCalls

		lastCallTimeStr := ""
		if stat.LastCallTime != nil {
			lastCallTimeStr = stat.LastCallTime.Format("2006-01-02 15:04:05")
		}

		statsList = append(statsList, map[string]interface{}{
			"skill_name":     stat.SkillName,
			"total_calls":    stat.TotalCalls,
			"success_calls":  stat.SuccessCalls,
			"failed_calls":   stat.FailedCalls,
			"last_call_time": lastCallTimeStr,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total_skills":  len(skillList),
		"total_calls":   totalCalls,
		"total_success": totalSuccess,
		"total_failed":  totalFailed,
		"skills_dir":    skillsDir,
		"stats":         statsList,
	})
}

// ClearSkillStats clears all Skills statistics
func (h *SkillsHandler) ClearSkillStats(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the database connection is not configured"})
		return
	}

	if err := h.db.ClearSkillStats(); err != nil {
		h.logger.Error("failed to clear Skills statistics", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear statistics: " + err.Error()})
		return
	}

	h.logger.Info("all Skills statistics have been cleared")
	c.JSON(http.StatusOK, gin.H{
		"message": "All Skills statistics have been cleared",
	})
}

// ClearSkillStatsByName clears the statistics of the specified skill
func (h *SkillsHandler) ClearSkillStatsByName(c *gin.Context) {
	skillName := c.Param("name")
	if skillName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the skill name cannot be empty"})
		return
	}

	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the database connection is not configured"})
		return
	}

	if err := h.db.ClearSkillStatsByName(skillName); err != nil {
		h.logger.Error("failed to clear the specified skill's statistics", zap.String("skill", skillName), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear statistics: " + err.Error()})
		return
	}

	h.logger.Info("the specified skill's statistics have been cleared", zap.String("skill", skillName))
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Cleared statistics for skill '%s'", skillName),
	})
}

// removeSkillFromRoles reserved: roles no longer store skill bindings, no-op.
func (h *SkillsHandler) removeSkillFromRoles(skillName string) []string {
	_ = skillName
	return nil
}

// saveRolesConfig saves the roles configuration to a file (called from SkillsHandler)
func (h *SkillsHandler) saveRolesConfig() error {
	configDir := filepath.Dir(h.configPath)
	rolesDir := h.config.RolesDir
	if rolesDir == "" {
		rolesDir = "roles" // default directory
	}

	// if it is a relative path, it is relative to the directory of the config file
	if !filepath.IsAbs(rolesDir) {
		rolesDir = filepath.Join(configDir, rolesDir)
	}

	// ensure the directory exists
	if err := os.MkdirAll(rolesDir, 0755); err != nil {
		return fmt.Errorf("failed to create the role directory: %w", err)
	}

	// save each role to a separate file
	if h.config.Roles != nil {
		for roleName, role := range h.config.Roles {
			// ensure the role name is set correctly
			if role.Name == "" {
				role.Name = roleName
			}

			// use the role name as the file name (sanitized file name, avoiding special characters)
			safeFileName := sanitizeRoleFileName(role.Name)
			roleFile := filepath.Join(rolesDir, safeFileName+".yaml")

			// serialize the roles config to YAML
			roleData, err := yaml.Marshal(&role)
			if err != nil {
				h.logger.Error("failed to serialize the role config", zap.String("role", roleName), zap.Error(err))
				continue
			}

			// handle the icon field: ensure icon values containing \U are wrapped in quotes (YAML needs quotes to correctly parse Unicode escapes)
			roleDataStr := string(roleData)
			if role.Icon != "" && strings.HasPrefix(role.Icon, "\\U") {
				// match the icon: \UXXXXXXXX format (no quotes), excluding cases that already have quotes
				re := regexp.MustCompile(`(?m)^(icon:\s+)(\\U[0-9A-F]{8})(\s*)$`)
				roleDataStr = re.ReplaceAllString(roleDataStr, `${1}"${2}"${3}`)
				roleData = []byte(roleDataStr)
			}

			// write to file
			if err := os.WriteFile(roleFile, roleData, 0644); err != nil {
				h.logger.Error("failed to save the role config file", zap.String("role", roleName), zap.String("file", roleFile), zap.Error(err))
				continue
			}

			h.logger.Info("role config saved to file", zap.String("role", roleName), zap.String("file", roleFile))
		}
	}

	return nil
}

// sanitizeRoleFileName converts a role name into a safe file name
func sanitizeRoleFileName(name string) string {
	// replace potentially unsafe characters
	replacer := map[rune]string{
		'/':  "_",
		'\\': "_",
		':':  "_",
		'*':  "_",
		'?':  "_",
		'"':  "_",
		'<':  "_",
		'>':  "_",
		'|':  "_",
		' ':  "_",
	}

	var result []rune
	for _, r := range name {
		if replacement, ok := replacer[r]; ok {
			result = append(result, []rune(replacement)...)
		} else {
			result = append(result, r)
		}
	}

	fileName := string(result)
	// if the file name is empty, use the default name
	if fileName == "" {
		fileName = "role"
	}

	return fileName
}

// isValidSkillName validates the skill directory name (consistent with the name field of Agent Skills: lowercase, digits, hyphens)
func isValidSkillName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}
