package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"cyberstrike-ai/internal/attackchain"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AttackChainHandler attack chain handler
type AttackChainHandler struct {
	db           *database.DB
	logger       *zap.Logger
	openAIConfig *config.OpenAIConfig
	mu           sync.RWMutex // protects concurrent access to openAIConfig
	// used to prevent concurrent generation for the same conversation
	generatingLocks sync.Map // map[string]*sync.Mutex
}

// NewAttackChainHandler creates a new attack chain handler
func NewAttackChainHandler(db *database.DB, openAIConfig *config.OpenAIConfig, logger *zap.Logger) *AttackChainHandler {
	return &AttackChainHandler{
		db:           db,
		logger:       logger,
		openAIConfig: openAIConfig,
	}
}

// UpdateConfig updates the OpenAI configuration
func (h *AttackChainHandler) UpdateConfig(cfg *config.OpenAIConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.openAIConfig = cfg
	h.logger.Info("AttackChainHandler config updated",
		zap.String("base_url", cfg.BaseURL),
		zap.String("model", cfg.Model),
	)
}

// getOpenAIConfig gets the OpenAI configuration (thread-safe)
func (h *AttackChainHandler) getOpenAIConfig() *config.OpenAIConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.openAIConfig
}

// GetAttackChain gets the attack chain (generated on demand)
// GET /api/attack-chain/:conversationId
func (h *AttackChainHandler) GetAttackChain(c *gin.Context) {
	conversationID := c.Param("conversationId")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversationId is required"})
		return
	}

	// check whether the conversation exists
	_, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Warn("the conversation does not exist", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "the conversation does not exist"})
		return
	}

	// try loading from the database first (if already generated)
	openAIConfig := h.getOpenAIConfig()
	builder := attackchain.NewBuilder(h.db, openAIConfig, h.logger)
	chain, err := builder.LoadCachedChainIfPresent(conversationID)
	if err == nil && chain != nil {
		// if it already exists, return directly
		h.logger.Info("returning the existing attack chain", zap.String("conversationId", conversationID))
		c.JSON(http.StatusOK, chain)
		return
	}

	// if it does not exist, generate a new attack chain (on demand)
	// use a lock mechanism to prevent concurrent generation for the same conversation
	lockInterface, _ := h.generatingLocks.LoadOrStore(conversationID, &sync.Mutex{})
	lock := lockInterface.(*sync.Mutex)

	// try to acquire the lock; return an error if generation is in progress
	acquired := lock.TryLock()
	if !acquired {
		h.logger.Info("the attack chain is being generated, please try again later", zap.String("conversationId", conversationID))
		c.JSON(http.StatusConflict, gin.H{"error": "the attack chain is being generated, please try again later"})
		return
	}
	defer lock.Unlock()

	// check again whether it has been generated (it may have finished generating while waiting for the lock)
	chain, err = builder.LoadCachedChainIfPresent(conversationID)
	if err == nil && chain != nil {
		h.logger.Info("returning the existing attack chain (generated while waiting for the lock)", zap.String("conversationId", conversationID))
		c.JSON(http.StatusOK, chain)
		return
	}

	h.logger.Info("starting to generate the attack chain", zap.String("conversationId", conversationID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	chain, err = builder.BuildChainFromConversation(ctx, conversationID)
	if err != nil {
		h.logger.Error("failed to generate the attack chain", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate the attack chain: " + err.Error()})
		return
	}

	// after generation finishes, delete it from the lock map (optional; keeping it can also prevent duplicate generation in a short time)
	// h.generatingLocks.Delete(conversationID)

	c.JSON(http.StatusOK, chain)
}

// RegenerateAttackChain regenerates the attack chain
// POST /api/attack-chain/:conversationId/regenerate
func (h *AttackChainHandler) RegenerateAttackChain(c *gin.Context) {
	conversationID := c.Param("conversationId")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversationId is required"})
		return
	}

	// check whether the conversation exists
	_, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Warn("the conversation does not exist", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "the conversation does not exist"})
		return
	}

	// use a lock mechanism to prevent concurrent generation
	lockInterface, _ := h.generatingLocks.LoadOrStore(conversationID, &sync.Mutex{})
	lock := lockInterface.(*sync.Mutex)

	acquired := lock.TryLock()
	if !acquired {
		h.logger.Info("the attack chain is being generated, please try again later", zap.String("conversationId", conversationID))
		c.JSON(http.StatusConflict, gin.H{"error": "the attack chain is being generated, please try again later"})
		return
	}
	defer lock.Unlock()

	// only the generation request holding the lock may delete the old image; a request that returns 409 must not modify the cache.
	if err := h.db.DeleteAttackChain(conversationID); err != nil {
		h.logger.Warn("failed to delete the old attack chain", zap.Error(err))
	}

	// generate a new attack chain
	h.logger.Info("regenerate the attack chain", zap.String("conversationId", conversationID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	openAIConfig := h.getOpenAIConfig()
	builder := attackchain.NewBuilder(h.db, openAIConfig, h.logger)
	chain, err := builder.BuildChainFromConversation(ctx, conversationID)
	if err != nil {
		h.logger.Error("failed to generate the attack chain", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate the attack chain: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, chain)
}
