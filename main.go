package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

type Config struct {
	PrometheusURL  string
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool
	MinioBucket    string
}

func loadConfig() (*Config, error) {
	cfg := &Config{
		PrometheusURL:  getEnvOrDefault("PROMETHEUS_URL", "http://localhost:9090"),
		MinioEndpoint:  getEnvOrDefault("MINIO_ENDPOINT", "localhost:9000"),
		MinioAccessKey: getEnvOrDefault("MINIO_ACCESS_KEY", "minioadmin"),
		MinioSecretKey: getEnvOrDefault("MINIO_SECRET_KEY", "minioadmin"),
		MinioBucket:    getEnvOrDefault("MINIO_BUCKET", "prometheus-snapshots"),
		MinioUseSSL: func() bool {
			if value, err := strconv.ParseBool(os.Getenv("MINIO_USE_SSL")); err == nil {
				return value
			}
			return false
		}(),
	}

	// Validate required configurations
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

// QueryRequest represents the structure of the request received from the web interface.
type QueryRequest struct {
	Query string `json:"query"`
	Start string `json:"start"` // Start time in RFC3339 or Unix timestamp
	End   string `json:"end"`   // End time in RFC3339 or Unix timestamp
	Step  string `json:"step"`  // Step duration (e.g., "1m" for 1 minute)
	Title string `json:"title"`
}

// PrometheusResponse represents the structure of the response from Prometheus.
type PrometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
			Values [][]interface{}   `json:"values"`
		} `json:"result"`
	} `json:"data"`
	Error     string `json:"error"`
	ErrorType string `json:"errorType"`
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
		http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode the JSON request body
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

	// Validate time range parameters
	if req.Start == "" || req.End == "" || req.Step == "" {
		http.Error(w, `{"error": "Start, End, and Step parameters are required"}`, http.StatusBadRequest)
		return
	}

	// Construct the Prometheus API endpoint URL
	prometheusQueryURL := fmt.Sprintf("%s/api/v1/query_range", config.PrometheusURL)

	// Create the request parameters for Prometheus
	params := url.Values{}
	params.Set("query", req.Query)
	params.Set("start", req.Start)
	params.Set("end", req.End)
	params.Set("step", req.Step)

	// Properly encode the URL
	fullURL := fmt.Sprintf("%s?%s", prometheusQueryURL, params.Encode())

	// Make the GET request to Prometheus
	resp, err := http.Get(fullURL)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to connect to Prometheus: %v"}`, err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Read the response body from Prometheus
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to read Prometheus response: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// Decode the Prometheus response
	var promResp PrometheusResponse
	err = json.Unmarshal(body, &promResp)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to decode Prometheus response: %v"}`, err), http.StatusInternalServerError)
		return
	}

	if promResp.Status != "success" {
		http.Error(w, fmt.Sprintf(`{"error": "Prometheus error: %s - %s"}`, promResp.ErrorType, promResp.Error), http.StatusInternalServerError)
		return
	}

	// Generate the visualization based on the Prometheus response
	img, err := generateVisualization(promResp, req.Title)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to generate visualization: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// Upload the image directly to MinIO/S3
	objectURL, err := uploadToMinIO(img, req.Title, r)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to upload PNG to MinIO: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// Respond with success message and MinIO URL
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"message": "Visualization successfully uploaded to MinIO", "url": "%s"}`, objectURL)))
}

func uploadToMinIO(img *bytes.Buffer, title string, r *http.Request) (string, error) {
	// Initialize MinIO client
	minioClient, err := minio.New(config.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.MinioAccessKey, config.MinioSecretKey, ""),
		Secure: config.MinioUseSSL,
	})
	if err != nil {
		return "", fmt.Errorf("failed to initialize MinIO client: %v", err)
	}

	// Use the configured bucket name
	bucketName := config.MinioBucket
	objectName := fmt.Sprintf("%s-%s.png", title, uuid.New().String())

	// Ensure the bucket exists
	exists, err := minioClient.BucketExists(r.Context(), bucketName)
	if err != nil {
		return "", fmt.Errorf("failed to check bucket existence: %v", err)
	}
	if !exists {
		err = minioClient.MakeBucket(r.Context(), bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to create bucket: %v", err)
		}
	}

	// Upload the image buffer directly
	_, err = minioClient.PutObject(r.Context(), bucketName, objectName, bytes.NewReader(img.Bytes()), int64(img.Len()), minio.PutObjectOptions{ContentType: "image/png"})
	if err != nil {
		return "", fmt.Errorf("failed to upload file to MinIO: %v", err)
	}

	// Generate a presigned URL valid for one week
	presignedURL, err := minioClient.PresignedGetObject(r.Context(), bucketName, objectName, time.Hour*24*7, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %v", err)
	}

	return presignedURL.String(), nil
}

func generateVisualization(resp PrometheusResponse, title string) (*bytes.Buffer, error) {
	if resp.Data.ResultType != "vector" && resp.Data.ResultType != "matrix" {
		return nil, fmt.Errorf("unsupported Prometheus result type: %s", resp.Data.ResultType)
	}

	p := plot.New()
	p.Title.Text = title
	p.X.Label.Text = "Time"
	p.Y.Label.Text = "Value"

	// Set the aspect ratio to 16:9
	width := vg.Points(800)  // 16 units
	height := vg.Points(450) // 9 units

	// Seed the random number generator for color generation
	rand.Seed(time.Now().UnixNano())

	if resp.Data.ResultType == "vector" {
		for _, result := range resp.Data.Result {
			if len(result.Value) == 2 {
				timestamp := float64(result.Value[0].(float64))
				value := result.Value[1].(string)
				floatValue, err := parseFloat(value)
				if err != nil {
					log.Printf("Error parsing value '%s': %v", value, err)
					continue
				}

				pts := make(plotter.XYs, 1)
				pts[0].X = timestamp
				pts[0].Y = floatValue

				line, err := plotter.NewLine(pts)
				if err != nil {
					return nil, fmt.Errorf("failed to create line plot: %v", err)
				}
				line.LineStyle.Width = vg.Points(2)
				line.LineStyle.Color = randomColor() // Assign a random color
				p.Add(line)
			}
		}
	} else if resp.Data.ResultType == "matrix" {
		for _, result := range resp.Data.Result {
			pts := make(plotter.XYs, len(result.Values))
			for i, valuePair := range result.Values {
				if len(valuePair) == 2 {
					timestamp := float64(valuePair[0].(float64))
					value := valuePair[1].(string)
					floatValue, err := parseFloat(value)
					if err != nil {
						log.Printf("Error parsing value '%s': %v", value, err)
						continue
					}
					pts[i].X = timestamp
					pts[i].Y = floatValue
				}
			}

			line, err := plotter.NewLine(pts)
			if err != nil {
				return nil, fmt.Errorf("failed to create line plot: %v", err)
			}
			line.LineStyle.Width = vg.Points(2)
			line.LineStyle.Color = randomColor() // Assign a random color
			p.Add(line)
		}
	}

	buffer := bytes.NewBuffer([]byte{})
	writer, err := p.WriterTo(width, height, "png") // Use the 16:9 aspect ratio
	if err != nil {
		return nil, fmt.Errorf("failed to create PNG writer: %v", err)
	}

	_, err = writer.WriteTo(buffer)
	if err != nil {
		return nil, fmt.Errorf("failed to write PNG to buffer: %v", err)
	}

	return buffer, nil
}

// randomColor generates a random color for the graph lines.
func randomColor() color.Color {
	return color.RGBA{
		R: uint8(rand.Intn(256)),
		G: uint8(rand.Intn(256)),
		B: uint8(rand.Intn(256)),
		A: 255,
	}
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
