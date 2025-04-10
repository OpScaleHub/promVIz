package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	PrometheusURL       string
	MinioEndpoint       string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioUseSSL         bool
	MinioBucket         string
	BrowserTimeout      time.Duration
	BrowserPath         string
	BrowserHeadless     bool
	BrowserScreenshotVP bool
	BrowserVPWidth      int
	BrowserVPHeight     int
}

func loadConfig() (*Config, error) {
	cfg := &Config{
		PrometheusURL:  getEnvOrDefault("PROMETHEUS_URL", "https://promethues.local.opscale.ir"),
		MinioEndpoint:  getEnvOrDefault("MINIO_ENDPOINT", "https://minio.local.opscale.ir"),
		MinioAccessKey: getEnvOrDefault("MINIO_ACCESS_KEY", "prometheus-snapshotter"),
		MinioSecretKey: getEnvOrDefault("MINIO_SECRET_KEY", "9aa5782a-9e0c-4396-99ae-c8955f03f88c"),
		MinioBucket:    getEnvOrDefault("MINIO_BUCKET", "prometheus-snapshots"),
		MinioUseSSL: func() bool {
			_, err := strconv.ParseBool(os.Getenv("MINIO_USE_SSL"))
			if err != nil {
				return false
			}
			return false
		}(),
		BrowserTimeout: func() time.Duration {
			_, err := time.ParseDuration(os.Getenv("BROWSER_TIMEOUT"))
			if err != nil {
				return 60 * time.Second
			}
			return 60 * time.Second
		}(),
		BrowserPath: getEnvOrDefault("BROWSER_PATH", ""),
		BrowserHeadless: func() bool {
			_, err := strconv.ParseBool(os.Getenv("BROWSER_HEADLESS"))
			if err != nil {
				return true
			}
			return true
		}(),
		BrowserScreenshotVP: func() bool {
			_, err := strconv.ParseBool(os.Getenv("BROWSER_SCREENSHOT_VP"))
			if err != nil {
				return false
			}
			return false
		}(),
		BrowserVPWidth: func() int {
			_, err := strconv.Atoi(os.Getenv("BROWSER_VP_WIDTH"))
			if err != nil {
				return 1920
			}
			return 1920
		}(),
		BrowserVPHeight: func() int {
			_, err := strconv.Atoi(os.Getenv("BROWSER_VP_HEIGHT"))
			if err != nil {
				return 1080
			}
			return 1080
		}(),
	}

	cfg.MinioEndpoint = strings.TrimPrefix(strings.TrimPrefix(cfg.MinioEndpoint, "http://"), "https://")

	if cfg.PrometheusURL == "" {
		return nil, fmt.Errorf("PROMETHEUS_URL is required")
	}
	if cfg.MinioEndpoint == "" {
		return nil, fmt.Errorf("MINIO_ENDPOINT is required")
	}
	if cfg.MinioAccessKey == "" {
		return nil, fmt.Errorf("MINIO_ACCESS_KEY is required")
	}
	if cfg.MinioSecretKey == "" {
		return nil, fmt.Errorf("MINIO_SECRET_KEY is required")
	}

	return cfg, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

var config *Config

type QueryRequest struct {
	Query string `json:"query"`
	Start string `json:"start"`
	End   string `json:"end"`
	Title string `json:"title"`
}

func parseAndFormatTime(input string) (string, error) {
	t, err := time.Parse(time.RFC3339, input)
	if err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}

	unixTime, err := strconv.ParseInt(input, 10, 64)
	if err == nil {
		t = time.Unix(unixTime, 0)
		return t.UTC().Format(time.RFC3339), nil
	}

	unixTimeFloat, err := strconv.ParseFloat(input, 64)
	if err == nil {
		sec, dec := int64(unixTimeFloat), int64((unixTimeFloat-float64(int64(unixTimeFloat)))*1e9)
		t = time.Unix(sec, dec)
		return t.UTC().Format(time.RFC3339), nil
	}

	return "", fmt.Errorf("failed to parse time '%s' as RFC3339 or Unix timestamp", input)
}

func main() {
	var err error
	config, err = loadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	http.HandleFunc("/", queryHandler)
	fmt.Println("Server listening on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func queryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Only POST requests are allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req QueryRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to decode request body: %v"}`, err), http.StatusBadRequest)
		return
	}

	if req.Query == "" {
		http.Error(w, `{"error": "Query parameter cannot be empty"}`, http.StatusBadRequest)
		return
	}

	startTimeFormatted, err := parseAndFormatTime(req.Start)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Invalid start time format: %v"}`, err), http.StatusBadRequest)
		return
	}
	endTimeFormatted, err := parseAndFormatTime(req.End)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Invalid end time format: %v"}`, err), http.StatusBadRequest)
		return
	}

	prometheusGraphURL, err := buildPrometheusGraphURL(config.PrometheusURL, req.Query, startTimeFormatted, endTimeFormatted)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to build Prometheus URL: %v"}`, err), http.StatusInternalServerError)
		return
	}
	log.Printf("Constructed Prometheus Graph URL: %s", prometheusGraphURL)

	ctx, cancel := context.WithTimeout(context.Background(), config.BrowserTimeout)
	defer cancel()

	screenshotBytes, err := takeScreenshotWithRod(ctx, prometheusGraphURL)
	if err != nil {
		log.Printf("Error taking screenshot: %v", err)
		errMsg := fmt.Sprintf(`{"error": "Failed to take screenshot: %v"}`, err)
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = fmt.Sprintf(`{"error": "Failed to take screenshot within %s timeout: %v"}`, config.BrowserTimeout, err)
		}
		http.Error(w, errMsg, http.StatusInternalServerError)
		return
	}

	minioCtx, minioCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer minioCancel()

	objectURL, err := uploadToMinIO(minioCtx, bytes.NewBuffer(screenshotBytes), req.Title)
	if err != nil {
		log.Printf("Error uploading to MinIO: %v", err)
		errMsg := fmt.Sprintf(`{"error": "Failed to upload PNG to MinIO: %v"}`, err)
		if minioCtx.Err() == context.DeadlineExceeded {
			errMsg = fmt.Sprintf(`{"error": "Failed to upload PNG to Minio within timeout: %v"}`, err)
		}
		http.Error(w, errMsg, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"message": "Screenshot successfully uploaded to MinIO", "url": "%s"}`, objectURL)))
}

func buildPrometheusGraphURL(baseURL, query, startTime, endTime string) (string, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base Prometheus URL: %w", err)
	}

	if !strings.HasSuffix(parsedURL.Path, "/") {
		parsedURL.Path += "/"
	}
	parsedURL.Path += "graph"

	params := url.Values{}
	params.Set("g0.expr", query)
	params.Set("g0.tab", "0")
	params.Set("g0.range_input", "1h")
	params.Set("g0.start_time", startTime)
	params.Set("g0.end_time", endTime)

	parsedURL.RawQuery = params.Encode()

	return parsedURL.String(), nil
}

func takeScreenshotWithRod(ctx context.Context, targetURL string) ([]byte, error) {
	l := launcher.New().Leakless(false)

	if config.BrowserPath != "" {
		l.Bin(config.BrowserPath)
	}
	if !config.BrowserHeadless {
		l.Headless(false).Devtools(true)
	} else {
		l.Headless(true)
		l.Set("no-sandbox")
		l.Set("disable-setuid-sandbox")
		l.Set("disable-dev-shm-usage")
		l.Set("disable-gpu")
	}

	browserURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	browser := rod.New().ControlURL(browserURL)
	err = browser.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to browser (%s): %w", browserURL, err)
	}
	defer func() {
		closeErr := browser.Close()
		if closeErr != nil {
			log.Printf("Warning: failed to close browser: %v", closeErr)
		}
	}()

	var page *rod.Page
	pageErr := rod.Try(func() {
		page = browser.MustPage()
		page = page.Context(ctx)

		log.Printf("Setting viewport to %dx%d", config.BrowserVPWidth, config.BrowserVPHeight)
		// *** CORRECTED: Use config values directly ***
		err = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
			Width:             config.BrowserVPWidth,
			Height:            config.BrowserVPHeight,
			DeviceScaleFactor: 1,
			Mobile:            false,
		})
		if err != nil {
			if ctx.Err() != nil {
				panic(fmt.Errorf("context cancelled/timed out while setting viewport: %w", ctx.Err()))
			}
			panic(fmt.Errorf("failed to set viewport: %w", err))
		}

		log.Printf("Navigating to %s", targetURL)
		err = page.Navigate(targetURL)
		if err != nil {
			if ctx.Err() != nil {
				panic(fmt.Errorf("context cancelled/timed out during navigation to %s: %w", targetURL, ctx.Err()))
			}
			panic(fmt.Errorf("failed to navigate to URL %s: %w", targetURL, err))
		}

		log.Println("Waiting for page load...")
		err = page.WaitLoad()
		if err != nil {
			if ctx.Err() != nil {
				panic(fmt.Errorf("context cancelled/timed out during WaitLoad: %w", ctx.Err()))
			}
			log.Printf("Warning: page.WaitLoad failed (continuing anyway): %v", err)
		}

		log.Println("Waiting for network idle...")
		idleDuration := 5 * time.Second
		deadline, hasDeadline := ctx.Deadline()
		if hasDeadline {
			remaining := time.Until(deadline)
			if remaining < idleDuration+2*time.Second {
				idleDuration = remaining / 2
				if idleDuration < 1*time.Second {
					idleDuration = 1 * time.Second
				}
			}
		}
		if idleDuration <= 0 {
			log.Printf("Warning: Not enough time remaining for WaitIdle, skipping.")
		} else {
			err = page.WaitIdle(idleDuration)
			if err != nil {
				if ctx.Err() != nil {
					panic(fmt.Errorf("context cancelled/timed out during WaitIdle: %w", ctx.Err()))
				}
				log.Printf("Warning: page.WaitIdle failed (continuing anyway): %v", err)
			}
		}

	})

	if page != nil {
		defer func() {
			closeErr := page.Close()
			if closeErr != nil {
				log.Printf("Warning: failed to close page: %v", closeErr)
			}
		}()
	}

	if pageErr != nil {
		return nil, fmt.Errorf("browser operation failed: %w", pageErr)
	}

	log.Println("Taking screenshot...")
	var screenshotBytes []byte
	var screenshotErr error

	screenshotOptions := proto.PageCaptureScreenshot{
		Format:      proto.PageCaptureScreenshotFormatPng,
		Quality:     nil,
		FromSurface: true,
	}

	if config.BrowserScreenshotVP {
		log.Println("Screenshotting viewport only.")
		screenshotOptions.Clip = &proto.PageViewport{
			X:      0,
			Y:      0,
			Width:  float64(config.BrowserVPWidth),
			Height: float64(config.BrowserVPHeight),
			Scale:  1,
		}
		screenshotBytes, screenshotErr = page.Screenshot(false, &screenshotOptions)
	} else {
		log.Println("Screenshotting full page.")
		screenshotBytes, screenshotErr = page.Screenshot(true, &screenshotOptions)
	}

	if screenshotErr != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context cancelled/timed out during screenshot: %w", ctx.Err())
		}
		return nil, fmt.Errorf("failed to take screenshot: %w", screenshotErr)
	}

	if len(screenshotBytes) == 0 {
		return nil, fmt.Errorf("screenshot resulted in empty image")
	}

	log.Printf("Screenshot taken successfully (%d bytes).", len(screenshotBytes))
	return screenshotBytes, nil
}

func uploadToMinIO(ctx context.Context, img *bytes.Buffer, title string) (string, error) {
	minioClient, err := minio.New(config.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.MinioAccessKey, config.MinioSecretKey, ""),
		Secure: config.MinioUseSSL,
	})
	if err != nil {
		return "", fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	bucketName := config.MinioBucket
	safeTitle := strings.ReplaceAll(strings.ToLower(title), " ", "-")
	if safeTitle == "" {
		safeTitle = "snapshot"
	}
	objectName := fmt.Sprintf("%s-%s.png", safeTitle, uuid.New().String())

	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("context deadline exceeded before checking bucket: %w", ctx.Err())
		}
		return "", fmt.Errorf("failed to check bucket existence '%s': %w", bucketName, err)
	}
	if !exists {
		log.Printf("Bucket '%s' does not exist. Attempting to create it.", bucketName)
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("context deadline exceeded before creating bucket: %w", ctx.Err())
			}
			minioErr, ok := err.(minio.ErrorResponse)
			if ok && minioErr.Code == "BucketAlreadyOwnedByYou" {
				log.Printf("Bucket '%s' already exists (owned by you).", bucketName)
			} else {
				return "", fmt.Errorf("failed to create bucket '%s': %w", bucketName, err)
			}
		} else {
			log.Printf("Bucket '%s' created successfully.", bucketName)
		}
	}

	imgBytes := img.Bytes()
	imgSize := int64(len(imgBytes))
	log.Printf("Uploading %d bytes to %s/%s", imgSize, bucketName, objectName)

	_, err = minioClient.PutObject(ctx, bucketName, objectName, bytes.NewReader(imgBytes), imgSize, minio.PutObjectOptions{ContentType: "image/png"})
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("context deadline exceeded during upload: %w", ctx.Err())
		}
		return "", fmt.Errorf("failed to upload file to MinIO (%s/%s): %w", bucketName, objectName, err)
	}

	presignedURL, err := minioClient.PresignedGetObject(ctx, bucketName, objectName, time.Hour*24*7, nil)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("context deadline exceeded during presign URL generation: %w", ctx.Err())
		}
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	log.Printf("Successfully uploaded and generated presigned URL for %s/%s", bucketName, objectName)
	return presignedURL.String(), nil
}
