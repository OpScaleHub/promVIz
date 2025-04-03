package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

const prometheusURL = "http://localhost:9090"

// QueryRequest represents the structure of the request received from the web interface.
type QueryRequest struct {
	Query string `json:"query"`
	// Add other visualization parameters here, e.g.,
	// Start string `json:"start"`
	// End   string `json:"end"`
	// Step  string `json:"step"`
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
		http.Error(w, fmt.Sprintf("Failed to decode request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Query == "" {
		http.Error(w, "Query parameter cannot be empty", http.StatusBadRequest)
		return
	}

	// Construct the Prometheus API endpoint URL
	prometheusQueryURL := fmt.Sprintf("%s/api/v1/query", prometheusURL)

	// Create the request parameters for Prometheus
	params := url.Values{}
	params.Set("query", req.Query)
	// Add other parameters if needed, e.g., start, end, step

	// Make the POST request to Prometheus
	resp, err := http.PostForm(prometheusQueryURL, params)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to connect to Prometheus: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Read the response body from Prometheus
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read Prometheus response: %v", err), http.StatusInternalServerError)
		return
	}

	// Decode the Prometheus response
	var promResp PrometheusResponse
	err = json.Unmarshal(body, &promResp)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to decode Prometheus response: %v", err), http.StatusInternalServerError)
		return
	}

	if promResp.Status != "success" {
		http.Error(w, fmt.Sprintf("Prometheus error: %s - %s", promResp.ErrorType, promResp.Error), http.StatusInternalServerError)
		return
	}

	// Generate the visualization based on the Prometheus response
	img, err := generateVisualization(promResp, req.Title)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate visualization: %v", err), http.StatusInternalServerError)
		return
	}

	// Set the Content-Type header to image/png
	w.Header().Set("Content-Type", "image/png")

	// Write the PNG image to the response writer
	_, err = w.Write(img.Bytes())
	if err != nil {
		log.Printf("Failed to write image to response: %v", err)
	}
}

func generateVisualization(resp PrometheusResponse, title string) (*bytes.Buffer, error) {
	if resp.Data.ResultType != "vector" && resp.Data.ResultType != "matrix" {
		return nil, fmt.Errorf("unsupported Prometheus result type: %s", resp.Data.ResultType)
	}

	p := plot.New()
	p.Title.Text = title
	p.X.Label.Text = "Time"
	p.Y.Label.Text = "Value"

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
			p.Add(line)
		}
	}

	buffer := bytes.NewBuffer([]byte{})
	writer, err := p.WriterTo(vg.Points(400), vg.Points(300), "png")
	if err != nil {
		return nil, fmt.Errorf("failed to create PNG writer: %v", err)
	}

	_, err = writer.WriteTo(buffer)
	if err != nil {
		return nil, fmt.Errorf("failed to write PNG to buffer: %v", err)
	}

	return buffer, nil
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
