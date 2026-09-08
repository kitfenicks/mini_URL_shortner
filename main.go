package main

import (
        "database/sql"
        "log"
        "math/rand/v2"
        "net/http"
        "os"
        "strings"
        "net/url"
        "errors"

        "github.com/gin-gonic/gin"
        _ "github.com/lib/pq"
)

type Server struct {
        db *sql.DB
}

type LinkModel struct {
        ID          int    `json:"id"`
        OriginalURL string `json:"original_url"`
        ShortCode   string `json:"short_code"`
        Clicks      int    `json:"clicks"`
}

func main() {
        connStr := os.Getenv("DATABASE_URL")
        db, err := sql.Open("postgres", connStr)
        if err != nil {
                log.Fatalf("Failed to initialize database: %v", err)
        }
        defer db.Close()

        if err = db.Ping(); err != nil {
                log.Fatalf("Failed to connect to database: %v", err)
        }

        if err = initDatabase(db); err != nil {
                log.Fatalf("Failed to run database migrations: %v", err)
        }

        server := Server{db: db}

        r := gin.Default()

        r.POST("/shorten", server.createLink)
        r.GET("/:code", server.redirect)
        linksGroup := r.Group("/links")
        {
                linksGroup.GET("/", server.getLinks)
                linksGroup.GET("/:code", server.getLink)
                linksGroup.DELETE("/:code", server.deleteLink)
        }

        port := os.Getenv("PORT")

        log.Printf("Server is starting on port %s", port)
        if err := r.Run(":" + port); err != nil {
                log.Fatalf("Failed to start server: %v", err)
        }
}

func initDatabase(db *sql.DB) error {
        _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS links (
            id SERIAL PRIMARY KEY,
            original_url TEXT NOT NULL,
                        short_code VARCHAR(10) UNIQUE NOT NULL,
            clicks INTEGER DEFAULT 0
        )
    `)
        return err
}

func generateShortCode(length int) string {
        chars := "qwertyuiopasdfghjklzxcvbnmQWERTYUIOPASDFGHJKLZXCVBNM1234567890"

        result := ""
        for i := 0; i < length; i++ {
                randomIndex := rand.IntN(len(chars))
                result += string(chars[randomIndex])
        }
        return result
}

func (s *Server) createLink(c *gin.Context) {
        var req struct {
                URL string `json:"url" binding:"required"`
        }
        if err := c.ShouldBindJSON(&req); err != nil {
                c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
                return
        }

        cleanedURL, err := validateURL(req.URL)

        if err != nil{
          c.JSON(400, gin.H{"error": err.Error()})
          return
        }

        shortCode := generateShortCode(6)

        var link LinkModel

        query := `
        INSERT INTO links (original_url, short_code, clicks)
        VALUES ($1, $2, $3)
        RETURNING id, original_url, short_code, clicks;
        `

        err := s.db.QueryRow(query, cleanedURL, shortCode, 0).
                Scan(&link.ID, &link.OriginalURL, &link.ShortCode, &link.Clicks)

        if err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save link"})
                return
        }

        c.JSON(http.StatusCreated, link)
}

func (s *Server) redirect(c *gin.Context) {
        code := c.Param("code")

        if code == "" {
                c.JSON(http.StatusBadRequest, gin.H{"error": "Link code is missing"})
                return
        }

        query := `
        UPDATE links
        SET clicks = clicks + 1
        WHERE short_code = $1
        RETURNING original_url
        `
        var originalURL string
        err := s.db.QueryRow(query, code).
                Scan(&originalURL)

        if err != nil {
                if err == sql.ErrNoRows {
                        c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
                        return
                }
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
                return
        }

        c.Redirect(http.StatusFound, originalURL)
}

func (s *Server) getLinks(c *gin.Context) {
        rows, err := s.db.Query(`SELECT id, original_url, short_code, clicks FROM links ORDER BY id`)
        if err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
                return
        }
        defer rows.Close()

        var links []LinkModel

        for rows.Next() {
                var link LinkModel
                err := rows.Scan(&link.ID, &link.OriginalURL, &link.ShortCode, &link.Clicks)
                if err != nil {
                        c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
                        return
                }
                links = append(links, link)
        }
        if err = rows.Err(); err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
                return
        }
        if links == nil {
                links = []LinkModel{}
        }
        c.JSON(http.StatusOK, links)
}

func (s *Server) getLink(c *gin.Context) {
        code := c.Param("code")
        if code == "" {
                c.JSON(http.StatusBadRequest, gin.H{"error": "Link code cannot be empty"})
                return
        }

        var link LinkModel
        query := `
        SELECT id, original_url, short_code, clicks FROM links WHERE short_code = $1
        `
        err := s.db.QueryRow(query, code).
                Scan(&link.ID, &link.OriginalURL, &link.ShortCode, &link.Clicks)
        if err != nil {
                if err == sql.ErrNoRows {
                        c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
                        return
                }
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
                return
        }
        c.JSON(http.StatusOK, link)
}

func (s *Server) deleteLink(c *gin.Context) {
        code := c.Param("code")
        if code == "" {
                c.JSON(http.StatusBadRequest, gin.H{"error": "Code parameter is required"})
                return
        }

        query := `DELETE FROM links WHERE short_code = $1`

        result, err := s.db.Exec(query, code)
        if err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete link"})
                return
        }

        rowsAffected, err := result.RowsAffected()
        if err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get rows affected"})
                return
        }
        if rowsAffected == 0 {
                c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
                return
        }
        c.Status(http.StatusNoContent)
}

func validateURL (raw string) (string, error){
  cleaned := strings.TrimSpace(raw)

  if cleaned == "" {
    return "", errors.New("URL cannot be empty")
  }
  if len(cleaned) > 2048 {
    return "", errors.New("URL is too long")
  }

  u, err := url.ParseRequestURI(cleaned)
  if err != nil{
    return "", errors.New("InvalidURL")
  }

  if u.Scheme != "http" && u.Scheme != "https" {
    return "", errors.New("only http and https are allowed")
  }

  if u.Host == ""{
    return "", errors.New("host is missing")
  }

  return cleaned, nil
}