# Orders service — backend logic
def create_order(user_id: int, total: float) -> dict:
    return {"user_id": user_id, "total": total, "status": "pending"}

def list_orders(user_id: int) -> list:
    return []
