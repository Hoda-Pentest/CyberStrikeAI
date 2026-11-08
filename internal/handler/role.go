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

	"gopkg.in/yaml.v3"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// defaultRoleName is the persisted name of the built-in default role (matches the stored role name and its roles/ file).
// It is intentionally kept as the original Unicode value so existing data keeps working.
const defaultRoleName = "\u9ed8\u8ba4"

// RoleHandler role handler
type RoleHandler struct {
	config     *config.Config
	configPath string
	logger     *zap.Logger
	audit      *audit.Service
	db         *database.DB
}

func (h *RoleHandler) SetDB(db *database.DB) { h.db = db }

// SetAudit wires platform audit logging.
func (h *RoleHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewRoleHandler creates a new role handler
func NewRoleHandler(cfg *config.Config, configPath string, logger *zap.Logger) *RoleHandler {
	return &RoleHandler{
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

// GetRoles gets all roles
func (h *RoleHandler) GetRoles(c *gin.Context) {
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	roles := make([]config.RoleConfig, 0, len(h.config.Roles))
	for key, role := range h.config.Roles {
		// ensure the role's key matches its name
		if role.Name == "" {
			role.Name = key
		}
		roles = append(roles, role)
	}

	c.JSON(http.StatusOK, gin.H{
		"roles": roles,
	})
}

// GetRole gets a single role
func (h *RoleHandler) GetRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the role name cannot be empty"})
		return
	}

	if h.config.Roles == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "the role does not exist"})
		return
	}

	role, exists := h.config.Roles[roleName]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "the role does not exist"})
		return
	}

	// ensure the role's name matches its key
	if role.Name == "" {
		role.Name = roleName
	}

	c.JSON(http.StatusOK, gin.H{
		"role": role,
	})
}

// UpdateRole updates a role
func (h *RoleHandler) UpdateRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the role name cannot be empty"})
		return
	}

	var req config.RoleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameter: " + err.Error()})
		return
	}

	// ensure the role name matches the name in the request
	if req.Name == "" {
		req.Name = roleName
	}
	if err := h.validateRole(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name != roleName {
		if _, exists := h.config.Roles[req.Name]; exists {
			c.JSON(http.StatusConflict, gin.H{"error": "the role already exists"})
			return
		}
	}

	// initialize the Roles map
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	// delete old roles with the same name but a different key (avoid duplicates)
	// use the role name as the key to ensure uniqueness
	finalKey := req.Name
	keysToDelete := make([]string, 0)
	for key := range h.config.Roles {
		// if the key differs from the final key but the name is the same, mark it for deletion
		if key != finalKey {
			role := h.config.Roles[key]
			// ensure the role's name field is set correctly
			if role.Name == "" {
				role.Name = key
			}
			if role.Name == req.Name {
				keysToDelete = append(keysToDelete, key)
			}
		}
	}
	// delete the old role
	for _, key := range keysToDelete {
		delete(h.config.Roles, key)
		h.logger.Info("delete duplicate role", zap.String("oldKey", key), zap.String("name", req.Name))
	}

	// if the currently-updated key differs from the final key, the old one also needs to be deleted
	if roleName != finalKey {
		delete(h.config.Roles, roleName)
	}

	// if the role name changes, the old file needs to be deleted
	if roleName != finalKey {
		configDir := filepath.Dir(h.configPath)
		rolesDir := h.config.RolesDir
		if rolesDir == "" {
			rolesDir = "roles" // default directory
		}

		// if it is a relative path, it is relative to the directory of the config file
		if !filepath.IsAbs(rolesDir) {
			rolesDir = filepath.Join(configDir, rolesDir)
		}

		// delete the old role file
		oldSafeFileName := sanitizeFileName(roleName)
		oldRoleFileYaml := filepath.Join(rolesDir, oldSafeFileName+".yaml")
		oldRoleFileYml := filepath.Join(rolesDir, oldSafeFileName+".yml")

		if _, err := os.Stat(oldRoleFileYaml); err == nil {
			if err := os.Remove(oldRoleFileYaml); err != nil {
				h.logger.Warn("failed to delete the old role config file", zap.String("file", oldRoleFileYaml), zap.Error(err))
			}
		}
		if _, err := os.Stat(oldRoleFileYml); err == nil {
			if err := os.Remove(oldRoleFileYml); err != nil {
				h.logger.Warn("failed to delete the old role config file", zap.String("file", oldRoleFileYml), zap.Error(err))
			}
		}
	}

	// use the role name as the key to save (ensures uniqueness)
	h.config.Roles[finalKey] = req

	// save the configuration to a file
	if err := h.saveConfig(); err != nil {
		h.logger.Error("failed to save the configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the configuration: " + err.Error()})
		return
	}

	h.logger.Info("update role", zap.String("oldKey", roleName), zap.String("newKey", finalKey), zap.String("name", req.Name))
	if h.audit != nil {
		h.audit.RecordOK(c, "role", "update", "update role", "role", finalKey, map[string]interface{}{"name": req.Name})
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Role updated",
		"role":    req,
	})
}

// CreateRole creates a new role
func (h *RoleHandler) CreateRole(c *gin.Context) {
	var req config.RoleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameter: " + err.Error()})
		return
	}

	if err := h.validateRole(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// initialize the Roles map
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	// check whether the role already exists
	if _, exists := h.config.Roles[req.Name]; exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the role already exists"})
		return
	}

	// create a role (enabled by default)
	if !req.Enabled {
		req.Enabled = true
	}

	h.config.Roles[req.Name] = req

	// save the configuration to a file
	if err := h.saveConfig(); err != nil {
		h.logger.Error("failed to save the configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the configuration: " + err.Error()})
		return
	}

	h.logger.Info("create role", zap.String("roleName", req.Name))
	if h.audit != nil {
		h.audit.RecordOK(c, "role", "create", "create role", "role", req.Name, nil)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Role created",
		"role":    req,
	})
}

// DeleteRole deletes a role
func (h *RoleHandler) DeleteRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the role name cannot be empty"})
		return
	}

	if h.config.Roles == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "the role does not exist"})
		return
	}

	if _, exists := h.config.Roles[roleName]; !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "the role does not exist"})
		return
	}

	// The default role cannot be deleted
	if roleName == defaultRoleName {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the default role cannot be deleted"})
		return
	}

	delete(h.config.Roles, roleName)

	// delete the corresponding role file
	configDir := filepath.Dir(h.configPath)
	rolesDir := h.config.RolesDir
	if rolesDir == "" {
		rolesDir = "roles" // default directory
	}

	// if it is a relative path, it is relative to the directory of the config file
	if !filepath.IsAbs(rolesDir) {
		rolesDir = filepath.Join(configDir, rolesDir)
	}

	// try to delete the role files (.yaml and .yml)
	safeFileName := sanitizeFileName(roleName)
	roleFileYaml := filepath.Join(rolesDir, safeFileName+".yaml")
	roleFileYml := filepath.Join(rolesDir, safeFileName+".yml")

	// delete the .yaml file (if it exists)
	if _, err := os.Stat(roleFileYaml); err == nil {
		if err := os.Remove(roleFileYaml); err != nil {
			h.logger.Warn("failed to delete the role config file", zap.String("file", roleFileYaml), zap.Error(err))
		} else {
			h.logger.Info("role config file deleted", zap.String("file", roleFileYaml))
		}
	}

	// delete the .yml file (if it exists)
	if _, err := os.Stat(roleFileYml); err == nil {
		if err := os.Remove(roleFileYml); err != nil {
			h.logger.Warn("failed to delete the role config file", zap.String("file", roleFileYml), zap.Error(err))
		} else {
			h.logger.Info("role config file deleted", zap.String("file", roleFileYml))
		}
	}

	h.logger.Info("delete role", zap.String("roleName", roleName))
	if h.audit != nil {
		h.audit.RecordOK(c, "role", "delete", "delete role", "role", roleName, nil)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Role deleted",
	})
}

// saveConfig saves the configuration to a file in the directory
func (h *RoleHandler) saveConfig() error {
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
			safeFileName := sanitizeFileName(role.Name)
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
				// use a negative lookahead to ensure there is no quote after it, or directly match the case with no quotes
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

// sanitizeFileName converts a role name into a safe file name
func sanitizeFileName(name string) string {
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

// updateRolesConfig updates the roles configuration
func updateRolesConfig(doc *yaml.Node, cfg config.RolesConfig) {
	root := doc.Content[0]
	rolesNode := ensureMap(root, "roles")

	// clear the existing roles
	if rolesNode.Kind == yaml.MappingNode {
		rolesNode.Content = nil
	}

	// add a new role (using name as the key to ensure uniqueness)
	if cfg.Roles != nil {
		// build a map keyed by name first, deduplicating (keep the last one)
		rolesByName := make(map[string]config.RoleConfig)
		for roleKey, role := range cfg.Roles {
			// ensure the role's name field is set correctly
			if role.Name == "" {
				role.Name = roleKey
			}
			// use name as the final key; if multiple keys map to the same name, keep only the last one
			rolesByName[role.Name] = role
		}

		// write the deduplicated roles to YAML
		for roleName, role := range rolesByName {
			roleNode := ensureMap(rolesNode, roleName)
			setStringInMap(roleNode, "name", role.Name)
			setStringInMap(roleNode, "description", role.Description)
			setStringInMap(roleNode, "user_prompt", role.UserPrompt)
			if role.Icon != "" {
				setStringInMap(roleNode, "icon", role.Icon)
			}
			setBoolInMap(roleNode, "enabled", role.Enabled)

			// add the tool list (prefer the tools field)
			if len(role.Tools) > 0 {
				toolsNode := ensureArray(roleNode, "tools")
				toolsNode.Content = nil
				for _, toolKey := range role.Tools {
					toolNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: toolKey}
					toolsNode.Content = append(toolsNode.Content, toolNode)
				}
			} else if len(role.MCPs) > 0 {
				// backward compatibility: if there are no tools but there are mcps, save mcps
				mcpsNode := ensureArray(roleNode, "mcps")
				mcpsNode.Content = nil
				for _, mcpName := range role.MCPs {
					mcpNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: mcpName}
					mcpsNode.Content = append(mcpsNode.Content, mcpNode)
				}
			}
		}
	}
}

// ensureArray ensures an array node for the specified key exists in the array
func ensureArray(parent *yaml.Node, key string) *yaml.Node {
	_, valueNode := ensureKeyValue(parent, key)
	if valueNode.Kind != yaml.SequenceNode {
		valueNode.Kind = yaml.SequenceNode
		valueNode.Tag = "!!seq"
		valueNode.Content = nil
	}
	return valueNode
}
