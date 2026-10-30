import { NextResponse } from "next/server";

export function GET() {
  const response = new NextResponse(null, {
    status: 303,
    headers: { Location: "/login", "Cache-Control": "no-store" },
  });
  response.cookies.delete("obstor_token");
  return response;
}
