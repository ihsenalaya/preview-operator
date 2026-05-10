// OrderList — frontend component
import React from "react";

export function OrderList({ orders }: { orders: { id: number; total: number }[] }) {
  return (
    <ul>
      {orders.map((o) => (
        <li key={o.id}>Order #{o.id} — {o.total}€</li>
      ))}
    </ul>
  );
}
