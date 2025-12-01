package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"goshort/internal/domain"
	"goshort/internal/security"
	"goshort/internal/storage"

	"go.uber.org/zap"
)

type urlShortenerService struct {
	repo          storage.URLRepository
	cache         storage.CacheRepository
	rateLimiter   storage.RateLimiter
	ssrfValidator security.SSRFValidator
	logger        *zap.SugaredLogger
	shortCodeLen  int
	alphabet      string
}

// NewURLShortenerService creates a new URL shortener service
func NewURLShortenerService(
	repo storage.URLRepository,
	cache storage.CacheRepository,
	rateLimiter storage.RateLimiter,
	ssrfValidator security.SSRFValidator,
	logger *zap.SugaredLogger,
	shortCodeLen int,
	alphabet string,
) URLShortener {
	return &urlShortenerService{
		repo:          repo,
		cache:         cache,
		rateLimiter:   rateLimiter,
		ssrfValidator: ssrfValidator,
		logger:        logger,
		shortCodeLen:  shortCodeLen,
		alphabet:      alphabet,
	}
}

func (s *urlShortenerService) ShortenURL(ctx context.Context, originalURL, customCode, clientIP, userAgent string) (*domain.URL, error) {
	// Rate limiting check
	allowed, err := s.rateLimiter.Allow(ctx, clientIP)
	if err != nil {
		s.logger.Errorw("rate limiter error", "error", err, "ip", clientIP)
	}
	if !allowed {
		s.logger.Warnw("rate limit exceeded", "ip", clientIP)
		return nil, domain.ErrRateLimitExceeded
	}

	// SSRF validation
	if err := s.ssrfValidator.ValidateWithContext(ctx, originalURL); err != nil {
		s.logger.Warnw("SSRF validation failed",
			"url", originalURL,
			"error", err,
			"ip", clientIP,
		)
		return nil, fmt.Errorf("URL validation failed: %w", err)
	}

	// Generate or validate short code
	var shortCode string
	if customCode != "" {
		// Validate custom code
		if err := domain.ValidateShortCode(customCode); err != nil {
			return nil, err
		}
		
		// Check if exists
		exists, err := s.repo.Exists(ctx, customCode)
		if err != nil {
			return nil, fmt.Errorf("failed to check code existence: %w", err)
		}
		if exists {
			return nil, domain.ErrDuplicateShortCode
		}
		
		shortCode = customCode
	} else {
		// Generate unique short code
		shortCode, err = s.generateUniqueShortCode(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate short code: %w", err)
		}
	}

	// Create URL entity
	url, err := domain.NewURL(originalURL, shortCode, clientIP, userAgent)
	if err != nil {
		return nil, fmt.Errorf("failed to create URL entity: %w", err)
	}

	// Save to database
	if err := s.repo.Create(ctx, url); err != nil {
		s.logger.Errorw("failed to save URL to database",
			"error", err,
			"short_code", shortCode,
		)
		return nil, fmt.Errorf("failed to save URL: %w", err)
	}

	// Cache the URL (ignore cache errors)
	cacheKey := fmt.Sprintf("url:%s", shortCode)
	if err := s.cache.Set(ctx, cacheKey, url.OriginalURL, 3600); err != nil {
		s.logger.Warnw("failed to cache URL", "error", err, "short_code", shortCode)
	}

	s.logger.Infow("URL shortened successfully",
		"short_code", shortCode,
		"original_url", originalURL,
		"ip", clientIP,
	)

	return url, nil
}

func (s *urlShortenerService) GetOriginalURL(ctx context.Context, shortCode string) (*domain.URL, error) {
	// Validate short code format
	if err := domain.ValidateShortCode(shortCode); err != nil {
		return nil, err
	}

	// Try cache first
	cacheKey := fmt.Sprintf("url:%s", shortCode)
	cachedURL, err := s.cache.Get(ctx, cacheKey)
	if err == nil && cachedURL != "" {
		// Increment click count in cache (async)
		go func() {
			if err := s.cache.IncrementClickCount(context.Background(), shortCode); err != nil {
				s.logger.Warnw("failed to increment cache click count", "error", err)
			}
		}()

		// Return from cache
		url := &domain.URL{
			ShortCode:   shortCode,
			OriginalURL: cachedURL,
		}
		return url, nil
	}

	// Get from database
	url, err := s.repo.GetByShortCode(ctx, shortCode)
	if err != nil {
		return nil, err
	}

	// Check if expired or inactive
	if url.IsExpired() {
		return nil, domain.ErrURLExpired
	}
	if !url.IsActive {
		return nil, domain.ErrURLInactive
	}

	// Increment click count (async)
	go func() {
		ctx := context.Background()
		if err := s.repo.IncrementClickCount(ctx, shortCode); err != nil {
			s.logger.Warnw("failed to increment DB click count", "error", err)
		}
		if err := s.cache.IncrementClickCount(ctx, shortCode); err != nil {
			s.logger.Warnw("failed to increment cache click count", "error", err)
		}
	}()

	// Update cache
	if err := s.cache.Set(ctx, cacheKey, url.OriginalURL, 3600); err != nil {
		s.logger.Warnw("failed to update cache", "error", err)
	}

	return url, nil
}

func (s *urlShortenerService) GetURLDetails(ctx context.Context, shortCode string) (*domain.URL, error) {
	if err := domain.ValidateShortCode(shortCode); err != nil {
		return nil, err
	}

	url, err := s.repo.GetByShortCode(ctx, shortCode)
	if err != nil {
		return nil, err
	}

	// Get cached click count if available
	cachedClicks, err := s.cache.GetClickCount(ctx, shortCode)
	if err == nil && cachedClicks > url.ClickCount {
		url.ClickCount = cachedClicks
	}

	return url, nil
}

func (s *urlShortenerService) DeleteURL(ctx context.Context, id string) error {
	// Delete from database
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	// Get URL to find short code for cache invalidation
	url, err := s.repo.GetByID(ctx, id)
	if err == nil {
		cacheKey := fmt.Sprintf("url:%s", url.ShortCode)
		// Delete from cache (ignore errors)
		= s.cache.Delete(ctx, cacheKey)

	}

	s.logger.Infow("URL deleted", "id", id)
	return nil
}

func (s *urlShortenerService) ListURLs(ctx context.Context, limit, offset int) ([]*domain.URL, error) {
	// Validate pagination params
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	return s.repo.List(ctx, limit, offset)
}

// generateUniqueShortCode generates a unique short code
func (s *urlShortenerService) generateUniqueShortCode(ctx context.Context) (string, error) {
	maxAttempts := 10

	for i := 0; i < maxAttempts; i++ {
		code := s.generateRandomCode()

		// Check if exists
		exists, err := s.repo.Exists(ctx, code)
		if err != nil {
			return "", err
		}

		if !exists {
			return code, nil
		}
	}

	return "", fmt.Errorf("failed to generate unique short code after %d attempts", maxAttempts)
}

// generateRandomCode generates a cryptographically secure random code
func (s *urlShortenerService) generateRandomCode() string {
	code := make([]byte, s.shortCodeLen)
	alphabetLen := big.NewInt(int64(len(s.alphabet)))

	for i := 0; i < s.shortCodeLen; i++ {
		randomIndex, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			// Fallback to timestamp-based generation
			return fmt.Sprintf("%d", time.Now().UnixNano())
		}
		code[i] = s.alphabet[randomIndex.Int64()]
	}

	return string(code)
}

