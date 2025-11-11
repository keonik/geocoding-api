#!/bin/bash

# Test script to demonstrate flexible semantic search ordering
# Note: You'll need a valid API key to run these tests

API_KEY="your-valid-api-key-here"
BASE_URL="http://localhost:8080/api/v1/addresses/semantic"

echo "🔍 Testing Flexible Semantic Search Order Independence"
echo "=================================================="

# Test different orderings of the same address components
test_queries=(
    "123 main columbus"
    "columbus main 123" 
    "main 123 columbus"
    "columbus 123 main"
    "main columbus 123"
    "123 columbus main"
)

echo "Testing different orderings of '123 Main St Columbus':"
echo

for query in "${test_queries[@]}"; do
    echo "Query: '$query'"
    echo "curl -H \"Authorization: Bearer \$API_KEY\" \"$BASE_URL?q=$query&limit=3\""
    echo "---"
done

echo
echo "Additional flexible search examples:"
echo "- q='franklin county main' → Main streets in Franklin County"
echo "- q='43215 elm' → Elm streets in ZIP 43215"  
echo "- q='columbus broad' → Broad streets in Columbus"
echo "- q='main st ohio' → Main streets in Ohio"
echo
echo "The search now uses token-based matching so ANY order works!"
echo "Each token (word) is matched against house_number, street, city, county, and zip."
echo "Results are ranked by relevance with exact matches scoring highest."