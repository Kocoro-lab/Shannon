from fastapi.testclient import TestClient

from main import app


client = TestClient(app)


def test_tools_select_basic():
    # Ensure startup events ran and tools registered
    resp = client.get("/tools/list")
    assert resp.status_code == 200
    tools = resp.json()
    assert isinstance(tools, list)

    # Call selector without providers configured (heuristic fallback)
    body = {
        "task": "calculate 2 + 2 and search latest news",
        "exclude_dangerous": True,
        "max_tools": 2,
    }
    r = client.post("/tools/select", json=body)
    assert r.status_code == 200
    data = r.json()
    assert "selected_tools" in data
    assert "calls" in data
    assert isinstance(data["selected_tools"], list)
    assert isinstance(data["calls"], list)

    # Response should be cacheable and consistent on second call
    r2 = client.post("/tools/select", json=body)
    assert r2.status_code == 200
    data2 = r2.json()
    assert data == data2
