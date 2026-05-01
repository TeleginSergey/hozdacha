package middleware

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
)

var (
	// Опасные паттерны для XSS
	dangerousPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?i)javascript:`),
		regexp.MustCompile(`(?i)on\w+\s*=`),
		regexp.MustCompile(`(?i)vbscript:`),
		regexp.MustCompile(`(?i)data:text/html`),
	}
)

// SanitizeHTML санитизирует HTML контент
func SanitizeHTML(input string) string {
	// Удаляем опасные паттерны
	sanitized := input
	for _, pattern := range dangerousPatterns {
		sanitized = pattern.ReplaceAllString(sanitized, "")
	}
	
	// Экранируем оставшиеся HTML символы
	return html.EscapeString(sanitized)
}

// SanitizeFilename санитизирует имя файла
func SanitizeFilename(filename string) string {
	// Удаляем опасные символы
	filename = regexp.MustCompile(`[^a-zA-Z0-9._-]`).ReplaceAllString(filename, "")
	// Ограничиваем длину
	if len(filename) > 255 {
		filename = filename[:255]
	}
	return filename
}

// SanitizeURL санитизирует URL
func SanitizeURL(url string) string {
	// Проверяем, что это валидный URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return ""
	}
	// Удаляем опасные символы
	url = regexp.MustCompile(`[^a-zA-Z0-9:/?#\[\]@!$&'()*+,;=._~-]`).ReplaceAllString(url, "")
	return url
}

// SanitizeNumeric проверяет и возвращает безопасное число
func SanitizeNumeric(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case string:
		// Удаляем все нецифровые символы
		cleaned := strings.Map(func(r rune) rune {
			if unicode.IsDigit(r) {
				return r
			}
			return -1
		}, v)
		if len(cleaned) > 0 {
			var result int64
			_, err := fmt.Sscanf(cleaned, "%d", &result)
			if err == nil {
				return result, true
			}
		}
	}
	return 0, false
}

