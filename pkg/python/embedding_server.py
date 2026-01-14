#!/usr/bin/env python3
"""
Local Embedding Server

A lightweight HTTP server that serves embeddings using CodeRankEmbed or other
sentence-transformers models. Designed for local code search and retrieval.

Usage:
    python embedding_server.py [--port PORT] [--model MODEL]

Models:
    - nomic-ai/CodeRankEmbed (default, optimized for code)
    - nomic-ai/nomic-embed-text-v1.5 (general text)
"""

import argparse
import json
import logging
import os
import sys
import time
from http.server import HTTPServer, BaseHTTPRequestHandler
from typing import Optional

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)
logger = logging.getLogger(__name__)

# Model configuration
DEFAULT_MODEL = "nomic-ai/CodeRankEmbed"
DEFAULT_PORT = 11435  # Different from Ollama's 11434

# Global model instance (loaded once)
_model = None
_model_name = None
_dimensions = None


def load_model(model_name: str):
    """Load the embedding model (lazy, cached)."""
    global _model, _model_name, _dimensions

    if _model is not None and _model_name == model_name:
        return _model

    logger.info(f"Loading model: {model_name}")
    start = time.time()

    try:
        from sentence_transformers import SentenceTransformer
    except ImportError:
        logger.error("sentence-transformers not installed. Run: pip install sentence-transformers")
        sys.exit(1)

    _model = SentenceTransformer(model_name, trust_remote_code=True)
    _model_name = model_name
    _dimensions = _model.get_sentence_embedding_dimension()

    logger.info(f"Model loaded in {time.time() - start:.2f}s (dimensions: {_dimensions})")
    return _model


class EmbeddingHandler(BaseHTTPRequestHandler):
    """HTTP request handler for embedding requests."""

    def log_message(self, format, *args):
        """Override to use our logger."""
        logger.debug(f"{self.address_string()} - {format % args}")

    def send_json(self, data: dict, status: int = 200):
        """Send JSON response."""
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

    def do_GET(self):
        """Handle GET requests (health check)."""
        if self.path == "/health":
            self.send_json({
                "status": "ok",
                "model": _model_name,
                "dimensions": _dimensions,
            })
        elif self.path == "/info":
            self.send_json({
                "model": _model_name,
                "dimensions": _dimensions,
                "ready": _model is not None,
            })
        else:
            self.send_json({"error": "Not found"}, 404)

    def do_POST(self):
        """Handle POST requests (embeddings)."""
        if self.path != "/embed":
            self.send_json({"error": "Not found"}, 404)
            return

        try:
            # Read request body
            content_length = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(content_length)
            request = json.loads(body)

            texts = request.get("input", [])
            if not texts:
                self.send_json({"error": "No input texts provided"}, 400)
                return

            if isinstance(texts, str):
                texts = [texts]

            # Check if this is a code query (needs prefix for CodeRankEmbed)
            is_query = request.get("is_query", False)

            # Generate embeddings
            start = time.time()

            if _model_name == "nomic-ai/CodeRankEmbed" and is_query:
                # CodeRankEmbed requires prefix for queries
                texts = [f"Represent this query for searching relevant code: {t}" for t in texts]

            embeddings = _model.encode(texts, convert_to_numpy=True)

            duration = time.time() - start
            logger.debug(f"Embedded {len(texts)} texts in {duration:.3f}s")

            # Format response (similar to Voyage API)
            data = []
            for i, emb in enumerate(embeddings):
                data.append({
                    "embedding": emb.tolist(),
                    "index": i,
                })

            self.send_json({
                "data": data,
                "model": _model_name,
                "usage": {
                    "total_texts": len(texts),
                    "duration_ms": int(duration * 1000),
                },
            })

        except json.JSONDecodeError:
            self.send_json({"error": "Invalid JSON"}, 400)
        except Exception as e:
            logger.exception("Error processing request")
            self.send_json({"error": str(e)}, 500)


def main():
    parser = argparse.ArgumentParser(description="Local Embedding Server")
    parser.add_argument("--port", type=int, default=DEFAULT_PORT, help=f"Port to listen on (default: {DEFAULT_PORT})")
    parser.add_argument("--model", type=str, default=DEFAULT_MODEL, help=f"Model to use (default: {DEFAULT_MODEL})")
    parser.add_argument("--host", type=str, default="127.0.0.1", help="Host to bind to (default: 127.0.0.1)")
    args = parser.parse_args()

    # Load model on startup
    load_model(args.model)

    # Start server
    server = HTTPServer((args.host, args.port), EmbeddingHandler)
    logger.info(f"Embedding server listening on http://{args.host}:{args.port}")
    logger.info(f"Model: {_model_name} ({_dimensions} dimensions)")
    logger.info("Endpoints: GET /health, GET /info, POST /embed")

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logger.info("Shutting down...")
        server.shutdown()


if __name__ == "__main__":
    main()
