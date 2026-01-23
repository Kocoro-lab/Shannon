import { NextResponse } from "next/server";
import { hash } from "bcryptjs";
import { createClient } from "@supabase/supabase-js";
import { z } from "zod";

const registerSchema = z.object({
  name: z.string().min(1, "名前は必須です"),
  email: z.string().email("有効なメールアドレスを入力してください"),
  password: z.string().min(8, "パスワードは8文字以上必要です"),
});

function getSupabaseClient() {
  const supabaseUrl = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const serviceRoleKey = process.env.SUPABASE_SERVICE_ROLE_KEY;

  if (!supabaseUrl || !serviceRoleKey) {
    throw new Error("Missing Supabase environment variables");
  }

  return createClient(supabaseUrl, serviceRoleKey, {
    auth: {
      autoRefreshToken: false,
      persistSession: false,
    },
  });
}

export async function POST(request: Request) {
  try {
    const body = await request.json();
    const parsed = registerSchema.safeParse(body);

    if (!parsed.success) {
      const firstError = parsed.error.issues[0];
      return NextResponse.json(
        { error: firstError?.message ?? "入力内容に誤りがあります" },
        { status: 400 }
      );
    }

    const { name, email, password } = parsed.data;
    const supabase = getSupabaseClient();

    // Check if user already exists
    const { data: existingUser } = await supabase
      .from("users")
      .select("id")
      .eq("email", email)
      .single();

    if (existingUser) {
      return NextResponse.json(
        { error: "このメールアドレスは既に登録されています" },
        { status: 400 }
      );
    }

    // Hash password
    const hashedPassword = await hash(password, 12);

    // Create user
    const { data: user, error: userError } = await supabase
      .from("users")
      .insert({
        name,
        email,
        password: hashedPassword,
      })
      .select("id, email, name")
      .single();

    if (userError) {
      throw userError;
    }

    // Create profile
    const { error: profileError } = await supabase.from("profiles").insert({
      id: user.id,
      email: user.email,
      name: user.name,
    });

    if (profileError) {
      // Cleanup: delete user if profile creation fails
      await supabase.from("users").delete().eq("id", user.id);
      throw profileError;
    }

    return NextResponse.json(
      { message: "アカウントが作成されました", userId: user.id },
      { status: 201 }
    );
  } catch (error) {
    return NextResponse.json(
      { error: "登録中にエラーが発生しました" },
      { status: 500 }
    );
  }
}
