-- Analysis Files Schema for File Upload Feature
-- Run this in your Supabase SQL Editor

-- =============================================================================
-- Storage Bucket for Analysis Files
-- =============================================================================

-- Create storage bucket for analysis files (private)
INSERT INTO storage.buckets (id, name, public, file_size_limit, allowed_mime_types)
VALUES (
  'analysis-files',
  'analysis-files',
  false,
  26214400,  -- 25MB limit
  ARRAY[
    'application/pdf',
    'image/png',
    'image/jpeg',
    'image/jpg',
    'text/plain',
    'text/markdown',
    'text/csv',
    'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    'application/vnd.ms-excel'
  ]
) ON CONFLICT (id) DO NOTHING;

-- =============================================================================
-- Analysis Files Table
-- =============================================================================

CREATE TABLE IF NOT EXISTS public.analysis_files (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
  goal_id UUID REFERENCES public.goals(id) ON DELETE SET NULL,
  file_name TEXT NOT NULL,
  file_type TEXT NOT NULL,
  file_size INTEGER NOT NULL,
  storage_path TEXT NOT NULL,
  extracted_text TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- =============================================================================
-- Indexes for performance
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_analysis_files_user_id ON public.analysis_files(user_id);
CREATE INDEX IF NOT EXISTS idx_analysis_files_goal_id ON public.analysis_files(goal_id);
CREATE INDEX IF NOT EXISTS idx_analysis_files_created_at ON public.analysis_files(created_at);

-- =============================================================================
-- Row Level Security (RLS)
-- =============================================================================

ALTER TABLE public.analysis_files ENABLE ROW LEVEL SECURITY;

-- Analysis files policies
CREATE POLICY "Users can view own analysis files"
  ON public.analysis_files FOR SELECT
  USING (user_id = auth.uid());

CREATE POLICY "Users can create own analysis files"
  ON public.analysis_files FOR INSERT
  WITH CHECK (user_id = auth.uid());

CREATE POLICY "Users can update own analysis files"
  ON public.analysis_files FOR UPDATE
  USING (user_id = auth.uid());

CREATE POLICY "Users can delete own analysis files"
  ON public.analysis_files FOR DELETE
  USING (user_id = auth.uid());

-- =============================================================================
-- Storage Policies
-- =============================================================================

-- Allow users to upload files to their own folder
CREATE POLICY "Users can upload own files"
  ON storage.objects FOR INSERT
  WITH CHECK (
    bucket_id = 'analysis-files'
    AND auth.uid()::text = (storage.foldername(name))[1]
  );

-- Allow users to read their own files
CREATE POLICY "Users can read own files"
  ON storage.objects FOR SELECT
  USING (
    bucket_id = 'analysis-files'
    AND auth.uid()::text = (storage.foldername(name))[1]
  );

-- Allow users to delete their own files
CREATE POLICY "Users can delete own files"
  ON storage.objects FOR DELETE
  USING (
    bucket_id = 'analysis-files'
    AND auth.uid()::text = (storage.foldername(name))[1]
  );
