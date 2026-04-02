"""Grocery classification demo — runs the grocery-classify workflow."""

import asyncio
import json
import sys
import os

# Set project root so the engine finds agents/workflows/routers here
os.chdir(os.path.dirname(os.path.abspath(__file__)))

from agentic_engine import orchestrate


async def classify(item_name: str) -> None:
    print(f"\nClassifying: {item_name}")
    print("-" * 40)

    result = await orchestrate("grocery-classify", {"item_name": item_name})

    status = result.get("status", "unknown")
    output = result.get("result") or result.get("output")

    print(f"Status: {status}")
    print(json.dumps(output, indent=2))


async def main() -> None:
    items = sys.argv[1:] if len(sys.argv) > 1 else [
        "chicken breast",
        "paper towels",
        "canned tomatoes",
        "spinach",
    ]

    for item in items:
        await classify(item)


if __name__ == "__main__":
    asyncio.run(main())
