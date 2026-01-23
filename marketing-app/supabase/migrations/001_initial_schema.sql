-- Marketing App Database Schema
-- Run this in your Supabase SQL Editor

-- =============================================================================
-- Users Table (Email/Password Authentication)
-- =============================================================================

-- Users table with password support
CREATE TABLE IF NOT EXISTS public.users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT,
  email TEXT UNIQUE NOT NULL,
  password TEXT NOT NULL,
  image TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- =============================================================================
-- Application Schema
-- =============================================================================

-- User profiles (linked to users)
CREATE TABLE IF NOT EXISTS public.profiles (
  id UUID PRIMARY KEY REFERENCES public.users(id) ON DELETE CASCADE,
  email TEXT UNIQUE NOT NULL,
  name TEXT,
  avatar_url TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Goals table
CREATE TABLE IF NOT EXISTS public.goals (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  industry TEXT NOT NULL,
  deadline DATE NOT NULL,
  status TEXT DEFAULT 'active' CHECK (status IN ('active', 'completed', 'archived')),
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Goal KPIs table
CREATE TABLE IF NOT EXISTS public.goal_kpis (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  goal_id UUID NOT NULL REFERENCES public.goals(id) ON DELETE CASCADE,
  kpi_id TEXT NOT NULL,
  current_value NUMERIC NOT NULL DEFAULT 0,
  target_value NUMERIC NOT NULL,
  unit TEXT NOT NULL,
  is_custom BOOLEAN DEFAULT FALSE,
  custom_name TEXT,
  custom_category TEXT,
  lower_is_better BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Strategies table
CREATE TABLE IF NOT EXISTS public.strategies (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
  goal_id UUID REFERENCES public.goals(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  description TEXT,
  impact TEXT CHECK (impact IN ('high', 'medium', 'low')),
  effort TEXT CHECK (effort IN ('high', 'medium', 'low')),
  status TEXT DEFAULT 'proposed' CHECK (status IN ('proposed', 'approved', 'in_progress', 'completed', 'rejected')),
  priority INTEGER DEFAULT 0,
  research_background TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Strategy tasks table
CREATE TABLE IF NOT EXISTS public.strategy_tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  strategy_id UUID NOT NULL REFERENCES public.strategies(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT,
  status TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'in_progress', 'completed')),
  due_date DATE,
  assignee TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- =============================================================================
-- Indexes for performance
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_users_email ON public.users(email);
CREATE INDEX IF NOT EXISTS idx_goals_user_id ON public.goals(user_id);
CREATE INDEX IF NOT EXISTS idx_goals_status ON public.goals(status);
CREATE INDEX IF NOT EXISTS idx_goal_kpis_goal_id ON public.goal_kpis(goal_id);
CREATE INDEX IF NOT EXISTS idx_strategies_user_id ON public.strategies(user_id);
CREATE INDEX IF NOT EXISTS idx_strategies_goal_id ON public.strategies(goal_id);
CREATE INDEX IF NOT EXISTS idx_strategies_status ON public.strategies(status);
CREATE INDEX IF NOT EXISTS idx_strategy_tasks_strategy_id ON public.strategy_tasks(strategy_id);

-- =============================================================================
-- Row Level Security (RLS)
-- =============================================================================

-- Enable RLS on all application tables
ALTER TABLE public.users ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.goals ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.goal_kpis ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.strategies ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.strategy_tasks ENABLE ROW LEVEL SECURITY;

-- Users table - Service role only (for auth)
-- No direct access for authenticated users

-- Profiles policies
CREATE POLICY "Users can view own profile"
  ON public.profiles FOR SELECT
  USING (id = auth.uid());

CREATE POLICY "Users can update own profile"
  ON public.profiles FOR UPDATE
  USING (id = auth.uid());

-- Goals policies
CREATE POLICY "Users can view own goals"
  ON public.goals FOR SELECT
  USING (user_id = auth.uid());

CREATE POLICY "Users can create own goals"
  ON public.goals FOR INSERT
  WITH CHECK (user_id = auth.uid());

CREATE POLICY "Users can update own goals"
  ON public.goals FOR UPDATE
  USING (user_id = auth.uid());

CREATE POLICY "Users can delete own goals"
  ON public.goals FOR DELETE
  USING (user_id = auth.uid());

-- Goal KPIs policies (cascade from goals)
CREATE POLICY "Users can view own goal KPIs"
  ON public.goal_kpis FOR SELECT
  USING (goal_id IN (SELECT id FROM public.goals WHERE user_id = auth.uid()));

CREATE POLICY "Users can create own goal KPIs"
  ON public.goal_kpis FOR INSERT
  WITH CHECK (goal_id IN (SELECT id FROM public.goals WHERE user_id = auth.uid()));

CREATE POLICY "Users can update own goal KPIs"
  ON public.goal_kpis FOR UPDATE
  USING (goal_id IN (SELECT id FROM public.goals WHERE user_id = auth.uid()));

CREATE POLICY "Users can delete own goal KPIs"
  ON public.goal_kpis FOR DELETE
  USING (goal_id IN (SELECT id FROM public.goals WHERE user_id = auth.uid()));

-- Strategies policies
CREATE POLICY "Users can view own strategies"
  ON public.strategies FOR SELECT
  USING (user_id = auth.uid());

CREATE POLICY "Users can create own strategies"
  ON public.strategies FOR INSERT
  WITH CHECK (user_id = auth.uid());

CREATE POLICY "Users can update own strategies"
  ON public.strategies FOR UPDATE
  USING (user_id = auth.uid());

CREATE POLICY "Users can delete own strategies"
  ON public.strategies FOR DELETE
  USING (user_id = auth.uid());

-- Strategy tasks policies (cascade from strategies)
CREATE POLICY "Users can view own strategy tasks"
  ON public.strategy_tasks FOR SELECT
  USING (strategy_id IN (SELECT id FROM public.strategies WHERE user_id = auth.uid()));

CREATE POLICY "Users can create own strategy tasks"
  ON public.strategy_tasks FOR INSERT
  WITH CHECK (strategy_id IN (SELECT id FROM public.strategies WHERE user_id = auth.uid()));

CREATE POLICY "Users can update own strategy tasks"
  ON public.strategy_tasks FOR UPDATE
  USING (strategy_id IN (SELECT id FROM public.strategies WHERE user_id = auth.uid()));

CREATE POLICY "Users can delete own strategy tasks"
  ON public.strategy_tasks FOR DELETE
  USING (strategy_id IN (SELECT id FROM public.strategies WHERE user_id = auth.uid()));

-- =============================================================================
-- Updated_at trigger function
-- =============================================================================

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply trigger to all tables with updated_at column
CREATE TRIGGER update_users_updated_at
  BEFORE UPDATE ON public.users
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_profiles_updated_at
  BEFORE UPDATE ON public.profiles
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_goals_updated_at
  BEFORE UPDATE ON public.goals
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_goal_kpis_updated_at
  BEFORE UPDATE ON public.goal_kpis
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_strategies_updated_at
  BEFORE UPDATE ON public.strategies
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_strategy_tasks_updated_at
  BEFORE UPDATE ON public.strategy_tasks
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
