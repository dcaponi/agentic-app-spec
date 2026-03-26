package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	engine "github.com/dcaponi/agentic-app-spec/runtime/go"
)

func main() {
	engine.RegisterHandler("product_fetch", productFetchHandler)
	engine.RegisterHandler("quality_scoring", qualityScoringHandler)

	http.HandleFunc("/review", handleReview)
	http.HandleFunc("/quick-review", handleQuickReview)
	http.HandleFunc("/comparison", handleComparison)

	port := "8080"
	fmt.Printf("Listening on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleReview(w http.ResponseWriter, r *http.Request) {
	productIDStr := r.URL.Query().Get("product_id")
	if productIDStr == "" {
		http.Error(w, `{"error":"product_id query param required"}`, http.StatusBadRequest)
		return
	}

	productID, err := strconv.ParseFloat(productIDStr, 64)
	if err != nil {
		http.Error(w, `{"error":"product_id must be a number"}`, http.StatusBadRequest)
		return
	}

	envelope, err := engine.Orchestrate("product-review", map[string]interface{}{
		"product_id": productID,
	})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(envelope)
}

func handleQuickReview(w http.ResponseWriter, r *http.Request) {
	productIDStr := r.URL.Query().Get("product_id")
	if productIDStr == "" {
		http.Error(w, `{"error":"product_id query param required"}`, http.StatusBadRequest)
		return
	}

	productID, err := strconv.ParseFloat(productIDStr, 64)
	if err != nil {
		http.Error(w, `{"error":"product_id must be a number"}`, http.StatusBadRequest)
		return
	}

	envelope, err := engine.Orchestrate("quick-review", map[string]interface{}{
		"product_id": productID,
	})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(envelope)
}

func handleComparison(w http.ResponseWriter, r *http.Request) {
	productIDStr := r.URL.Query().Get("product_id")
	if productIDStr == "" {
		http.Error(w, `{"error":"product_id query param required"}`, http.StatusBadRequest)
		return
	}

	productID, err := strconv.Atoi(productIDStr)
	if err != nil {
		http.Error(w, `{"error":"product_id must be a number"}`, http.StatusBadRequest)
		return
	}

	// Fetch the product first so we have the data to feed the comparison agent
	fetchResult, err := productFetchHandler(map[string]interface{}{"product_id": float64(productID)})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	product, _ := fetchResult.Output.(map[string]interface{})
	if found, _ := product["found"].(bool); !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "product not found"})
		return
	}

	// Invoke just the comparison agent directly
	result, err := engine.InvokeAgent("comparison-researcher", map[string]interface{}{
		"product_name": product["title"],
		"category":     product["category"],
		"price":        product["price"],
		"brand":        product["brand"],
		"description":  product["description"],
	})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result.Output)
}

// productFetchHandler fetches product data from DummyJSON API
func productFetchHandler(input map[string]interface{}) (*engine.AgentResult, error) {
	productID := int(input["product_id"].(float64))

	resp, err := http.Get(fmt.Sprintf("https://dummyjson.com/products/%d", productID))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch product: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return &engine.AgentResult{
			Output: map[string]interface{}{
				"found": false,
				"error": fmt.Sprintf("product not found (status %d)", resp.StatusCode),
			},
		}, nil
	}

	var product map[string]interface{}
	if err := json.Unmarshal(body, &product); err != nil {
		return nil, fmt.Errorf("failed to parse product JSON: %w", err)
	}

	// Build reviews text from the product reviews
	reviewsText := ""
	if reviews, ok := product["reviews"].([]interface{}); ok {
		var parts []string
		for _, r := range reviews {
			if rev, ok := r.(map[string]interface{}); ok {
				rating := rev["rating"]
				comment := rev["comment"]
				reviewer := rev["reviewerName"]
				parts = append(parts, fmt.Sprintf("[%v/5 by %v] %v", rating, reviewer, comment))
			}
		}
		reviewsText = strings.Join(parts, "\n")
	}

	output := map[string]interface{}{
		"found":        true,
		"title":        product["title"],
		"brand":        product["brand"],
		"category":     product["category"],
		"price":        product["price"],
		"rating":       product["rating"],
		"description":  product["description"],
		"reviews_text": reviewsText,
	}

	return &engine.AgentResult{Output: output}, nil
}

// qualityScoringHandler scores the generated review article
func qualityScoringHandler(input map[string]interface{}) (*engine.AgentResult, error) {
	article, _ := input["review_article"].(map[string]interface{})
	analysis, _ := input["review_analysis"].(map[string]interface{})
	comparison, _ := input["comparison_report"].(map[string]interface{})

	// Structure score: does the article have all expected sections?
	structureScore := 0.0
	if article != nil {
		for _, field := range []string{"title", "overview", "pros_cons", "comparison", "verdict", "rating"} {
			if _, ok := article[field]; ok {
				structureScore += 1.0
			}
		}
		structureScore = (structureScore / 6.0) * 100
	}

	// Completeness score: were all upstream data sources used?
	completenessScore := 0.0
	if analysis != nil {
		completenessScore += 50
	}
	if comparison != nil {
		completenessScore += 50
	}

	// Tone score: basic heuristic — check if the article has reasonable length
	toneScore := 0.0
	if article != nil {
		overview, _ := article["overview"].(string)
		proscons, _ := article["pros_cons"].(string)
		verdict, _ := article["verdict"].(string)
		totalLen := len(overview) + len(proscons) + len(verdict)
		if totalLen > 500 {
			toneScore = 90
		} else if totalLen > 200 {
			toneScore = 70
		} else if totalLen > 50 {
			toneScore = 50
		} else {
			toneScore = 20
		}
	}

	overall := (structureScore + completenessScore + toneScore) / 3.0

	output := map[string]interface{}{
		"structure_score":    structureScore,
		"completeness_score": completenessScore,
		"tone_score":         toneScore,
		"overall":            overall,
	}

	return &engine.AgentResult{Output: output}, nil
}
