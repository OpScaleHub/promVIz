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
	"strings"
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

	// Remove any protocol prefix from MinIO endpoint
	cfg.MinioEndpoint = strings.TrimPrefix(strings.TrimPrefix(cfg.MinioEndpoint, "http://"), "https://")

	// Validate required configurations
	if cfg.PrometheusURL == "" {
		return nil, fmt.Errorf("PROMETHEUS_URL is required")
	}
	if cfg.MinioEndpoint == "" {
		return nil, fmt.Errorf("MINIO_ENDPOINT is required")
	}
	if cfg.MinioSecretKey == "" {
		return nil, fmt.Errorf("MINIO_SECRET_KEY is required")return nil, fmt.Errorf("MINIO_ACCESS_KEY is required")
	}	}
retKey == "" {
	return cfg, nil	return nil, fmt.Errorf("MINIO_SECRET_KEY is required")
}	}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}(key, defaultValue string) string {
	return defaultValueif value := os.Getenv(key); value != "" {
}		return value

var config *Config	return defaultValue

// QueryRequest represents the structure of the request received from the web interface.
type QueryRequest struct {
	Query string `json:"query"`
	Start string `json:"start"` // Start time in RFC3339 or Unix timestamp the web interface.
	End   string `json:"end"`   // End time in RFC3339 or Unix timestamp
	Step  string `json:"step"`  // Step duration (e.g., "1m" for 1 minute)
	Title string `json:"title"`Start string `json:"start"` // Start time in RFC3339 or Unix timestamp
}	End   string `json:"end"`   // End time in RFC3339 or Unix timestamp

// PrometheusResponse represents the structure of the response from Prometheus.
type PrometheusResponse struct {
	Status string `json:"status"`
	Data   struct {ructure of the response from Prometheus.
		ResultType string `json:"resultType"` struct {
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
			Values [][]interface{}   `json:"values"`ct {
		} `json:"result"`ring]string `json:"metric"`
	} `json:"data"`n:"value"`
	Error     string `json:"error"`alues"`
	ErrorType string `json:"errorType"`	} `json:"result"`
}	} `json:"data"`
ring `json:"error"`
func main() {ing `json:"errorType"`
	var err error
	config, err = loadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)ar err error
	}	config, err = loadConfig()

	http.HandleFunc("/", queryHandler) err)
	fmt.Println("Server listening on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}	http.HandleFunc("/", queryHandler)

func queryHandler(w http.ResponseWriter, r *http.Request) {8080", nil))
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
		returnnc queryHandler(w http.ResponseWriter, r *http.Request) {
	}	if r.Method != http.MethodPost {
sts are allowed", http.StatusMethodNotAllowed)
	// Decode the JSON request body
	var req QueryRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to decode request body: %v"}`, err), http.StatusBadRequest) QueryRequest
		returnrr := json.NewDecoder(r.Body).Decode(&req)
	}	if err != nil {
printf(`{"error": "Failed to decode request body: %v"}`, err), http.StatusBadRequest)
	if req.Query == "" {
		http.Error(w, `{"error": "Query parameter cannot be empty"}`, http.StatusBadRequest)
		return
	}	if req.Query == "" {
parameter cannot be empty"}`, http.StatusBadRequest)
	// Validate time range parameters
	if req.Start == "" || req.End == "" || req.Step == "" {
		http.Error(w, `{"error": "Start, End, and Step parameters are required"}`, http.StatusBadRequest)
		return/ Validate time range parameters
	}	if req.Start == "" || req.End == "" || req.Step == "" {
tep parameters are required"}`, http.StatusBadRequest)
	// Construct the Prometheus API endpoint URL
	prometheusQueryURL := fmt.Sprintf("%s/api/v1/query_range", config.PrometheusURL)	}

	// Create the request parameters for Prometheustheus API endpoint URL
	params := url.Values{}ntf("%s/api/v1/query_range", config.PrometheusURL)
	params.Set("query", req.Query)
	params.Set("start", req.Start)meters for Prometheus
	params.Set("end", req.End)
	params.Set("step", req.Step)	params.Set("query", req.Query)
art)
	// Properly encode the URL
	fullURL := fmt.Sprintf("%s?%s", prometheusQueryURL, params.Encode())	params.Set("step", req.Step)

	// Make the GET request to Prometheus
	resp, err := http.Get(fullURL)Sprintf("%s?%s", prometheusQueryURL, params.Encode())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to connect to Prometheus: %v"}`, err), http.StatusInternalServerError) the GET request to Prometheus
		returnesp, err := http.Get(fullURL)
	}
	defer resp.Body.Close()		http.Error(w, fmt.Sprintf(`{"error": "Failed to connect to Prometheus: %v"}`, err), http.StatusInternalServerError)

	// Read the response body from Prometheus
	body, err := io.ReadAll(resp.Body).Close()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to read Prometheus response: %v"}`, err), http.StatusInternalServerError) the response body from Prometheus
		returnody, err := io.ReadAll(resp.Body)
	}	if err != nil {
or": "Failed to read Prometheus response: %v"}`, err), http.StatusInternalServerError)
	// Decode the Prometheus response
	var promResp PrometheusResponse
	err = json.Unmarshal(body, &promResp)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to decode Prometheus response: %v"}`, err), http.StatusInternalServerError)mResp PrometheusResponse
		returnrr = json.Unmarshal(body, &promResp)
	}	if err != nil {
or": "Failed to decode Prometheus response: %v"}`, err), http.StatusInternalServerError)
	if promResp.Status != "success" {
		http.Error(w, fmt.Sprintf(`{"error": "Prometheus error: %s - %s"}`, promResp.ErrorType, promResp.Error), http.StatusInternalServerError)
		return
	}	if promResp.Status != "success" {
%s"}`, promResp.ErrorType, promResp.Error), http.StatusInternalServerError)
	// Generate the visualization based on the Prometheus response
	img, err := generateVisualization(promResp, req.Title)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to generate visualization: %v"}`, err), http.StatusInternalServerError)rate the visualization based on the Prometheus response
		returnmg, err := generateVisualization(promResp, req.Title)
	}	if err != nil {
ailed to generate visualization: %v"}`, err), http.StatusInternalServerError)
	// Upload the image directly to MinIO/S3
	objectURL, err := uploadToMinIO(img, req.Title, r)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to upload PNG to MinIO: %v"}`, err), http.StatusInternalServerError)ad the image directly to MinIO/S3
		returnbjectURL, err := uploadToMinIO(img, req.Title, r)
	}	if err != nil {
 to upload PNG to MinIO: %v"}`, err), http.StatusInternalServerError)
	// Respond with success message and MinIO URL
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"message": "Visualization successfully uploaded to MinIO", "url": "%s"}`, objectURL)))// Respond with success message and MinIO URL
}	w.Header().Set("Content-Type", "application/json")

func uploadToMinIO(img *bytes.Buffer, title string, r *http.Request) (string, error) {(`{"message": "Visualization successfully uploaded to MinIO", "url": "%s"}`, objectURL)))
	// Initialize MinIO client
	minioClient, err := minio.New(config.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.MinioAccessKey, config.MinioSecretKey, ""),.Buffer, title string, r *http.Request) (string, error) {
		Secure: config.MinioUseSSL, Initialize MinIO client
	})r := minio.New(config.MinioEndpoint, &minio.Options{
	if err != nil {SecretKey, ""),
		return "", fmt.Errorf("failed to initialize MinIO client: %v", err)Secure: config.MinioUseSSL,
	}	})

	// Use the configured bucket nameo initialize MinIO client: %v", err)
	bucketName := config.MinioBucket
	objectName := fmt.Sprintf("%s-%s.png", title, uuid.New().String())
t name
	// Ensure the bucket exists
	exists, err := minioClient.BucketExists(r.Context(), bucketName)mt.Sprintf("%s-%s.png", title, uuid.New().String())
	if err != nil {
		return "", fmt.Errorf("failed to check bucket existence: %v", err)/ Ensure the bucket exists
	}:= minioClient.BucketExists(r.Context(), bucketName)
	if !exists {
		err = minioClient.MakeBucket(r.Context(), bucketName, minio.MakeBucketOptions{})Errorf("failed to check bucket existence: %v", err)
		if err != nil {
			return "", fmt.Errorf("failed to create bucket: %v", err) !exists {
		}err = minioClient.MakeBucket(r.Context(), bucketName, minio.MakeBucketOptions{})
	}		if err != nil {
create bucket: %v", err)
	// Upload the image buffer directly
	_, err = minioClient.PutObject(r.Context(), bucketName, objectName, bytes.NewReader(img.Bytes()), int64(img.Len()), minio.PutObjectOptions{ContentType: "image/png"})
	if err != nil {
		return "", fmt.Errorf("failed to upload file to MinIO: %v", err)/ Upload the image buffer directly
	}	_, err = minioClient.PutObject(r.Context(), bucketName, objectName, bytes.NewReader(img.Bytes()), int64(img.Len()), minio.PutObjectOptions{ContentType: "image/png"})

	// Generate a presigned URL valid for one week
	presignedURL, err := minioClient.PresignedGetObject(r.Context(), bucketName, objectName, time.Hour*24*7, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %v", err)/ Generate a presigned URL valid for one week
	}	presignedURL, err := minioClient.PresignedGetObject(r.Context(), bucketName, objectName, time.Hour*24*7, nil)

	return presignedURL.String(), nil	return "", fmt.Errorf("failed to generate presigned URL: %v", err)
}	}

func generateVisualization(resp PrometheusResponse, title string) (*bytes.Buffer, error) {
	if resp.Data.ResultType != "vector" && resp.Data.ResultType != "matrix" {
		return nil, fmt.Errorf("unsupported Prometheus result type: %s", resp.Data.ResultType)
	}func generateVisualization(resp PrometheusResponse, title string) (*bytes.Buffer, error) {
sultType != "vector" && resp.Data.ResultType != "matrix" {
	p := plot.New()orf("unsupported Prometheus result type: %s", resp.Data.ResultType)
	p.Title.Text = title
	p.X.Label.Text = "Time"
	p.Y.Label.Text = "Value"	p := plot.New()

	// Set the aspect ratio to 16:9
	width := vg.Points(800)  // 16 units
	height := vg.Points(450) // 9 units

	// Seed the random number generator for color generationnits
	rand.Seed(time.Now().UnixNano())	height := vg.Points(450) // 9 units

	if resp.Data.ResultType == "vector" {lor generation
		for _, result := range resp.Data.Result {())
			if len(result.Value) == 2 {
				timestamp := float64(result.Value[0].(float64)){
				value := result.Value[1].(string)t {
				floatValue, err := parseFloat(value)lue) == 2 {
				if err != nil {
					log.Printf("Error parsing value '%s': %v", value, err)result.Value[1].(string)
					continueloatValue, err := parseFloat(value)
				}				if err != nil {
value '%s': %v", value, err)
				pts := make(plotter.XYs, 1)
				pts[0].X = timestamp
				pts[0].Y = floatValue

				line, err := plotter.NewLine(pts)stamp
				if err != nil {
					return nil, fmt.Errorf("failed to create line plot: %v", err)
				}
				line.LineStyle.Width = vg.Points(2)
				line.LineStyle.Color = randomColor() // Assign a random color, fmt.Errorf("failed to create line plot: %v", err)
				p.Add(line)}
			}	line.LineStyle.Width = vg.Points(2)
		}ssign a random color
	} else if resp.Data.ResultType == "matrix" {
		for _, result := range resp.Data.Result {
			pts := make(plotter.XYs, len(result.Values))
			for i, valuePair := range result.Values {ype == "matrix" {
				if len(valuePair) == 2 {
					timestamp := float64(valuePair[0].(float64))ult.Values))
					value := valuePair[1].(string)s {
					floatValue, err := parseFloat(value)) == 2 {
					if err != nil {
						log.Printf("Error parsing value '%s': %v", value, err)valuePair[1].(string)
						continueloatValue, err := parseFloat(value)
					}
					pts[i].X = timestamprsing value '%s': %v", value, err)
					pts[i].Y = floatValue	continue
				}	}
			}					pts[i].X = timestamp

			line, err := plotter.NewLine(pts)
			if err != nil {
				return nil, fmt.Errorf("failed to create line plot: %v", err)
			}
			line.LineStyle.Width = vg.Points(2)
			line.LineStyle.Color = randomColor() // Assign a random color, fmt.Errorf("failed to create line plot: %v", err)
			p.Add(line)}
		}	line.LineStyle.Width = vg.Points(2)
	}			line.LineStyle.Color = randomColor() // Assign a random color

	buffer := bytes.NewBuffer([]byte{})
	writer, err := p.WriterTo(width, height, "png") // Use the 16:9 aspect ratio
	if err != nil {
		return nil, fmt.Errorf("failed to create PNG writer: %v", err)uffer := bytes.NewBuffer([]byte{})
	}	writer, err := p.WriterTo(width, height, "png") // Use the 16:9 aspect ratio

	_, err = writer.WriteTo(buffer)t.Errorf("failed to create PNG writer: %v", err)
	if err != nil {
		return nil, fmt.Errorf("failed to write PNG to buffer: %v", err)
	}	_, err = writer.WriteTo(buffer)

	return buffer, nil	return nil, fmt.Errorf("failed to write PNG to buffer: %v", err)
}	}

// randomColor generates a random color for the graph lines.
func randomColor() color.Color {
	return color.RGBA{
		R: uint8(rand.Intn(256)),random color for the graph lines.
		G: uint8(rand.Intn(256)),lor {
		B: uint8(rand.Intn(256)),olor.RGBA{
		A: 255,R: uint8(rand.Intn(256)),
	}	G: uint8(rand.Intn(256)),
}		B: uint8(rand.Intn(256)),

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
