"use client";

import { useState, useCallback } from "react";
import { useDropzone } from "react-dropzone";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Upload,
  X,
  FileText,
  Image as ImageIcon,
  FileSpreadsheet,
  File,
  Loader2,
  AlertCircle,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { UploadedFile } from "@/lib/shannon/types";
import {
  ALLOWED_FILE_TYPES,
  MAX_FILE_SIZE_MB,
  MAX_FILES,
  FILE_EXTENSIONS,
} from "@/lib/shannon/types";
import {
  validateFile,
  formatFileSize,
  isImageFile,
  isPdfFile,
  isExcelFile,
  isCsvFile,
} from "@/lib/file-utils";
import { uploadFile, deleteFile } from "@/lib/supabase/files";

interface FileUploadProps {
  files: UploadedFile[];
  onFilesChange: (files: UploadedFile[]) => void;
  userId: string;
  goalId?: string;
  maxFiles?: number;
  disabled?: boolean;
}

interface UploadingFile {
  file: File;
  progress: number;
  error?: string;
}

export function FileUpload({
  files,
  onFilesChange,
  userId,
  goalId,
  maxFiles = MAX_FILES,
  disabled = false,
}: FileUploadProps) {
  const [uploadingFiles, setUploadingFiles] = useState<UploadingFile[]>([]);
  const [error, setError] = useState<string | null>(null);

  const handleUpload = useCallback(async (acceptedFiles: File[]) => {
    setError(null);

    const remainingSlots = maxFiles - files.length;
    if (remainingSlots <= 0) {
      setError(`最大${maxFiles}ファイルまでアップロードできます`);
      return;
    }

    const filesToUpload = acceptedFiles.slice(0, remainingSlots);

    for (const file of filesToUpload) {
      const validation = validateFile(file);
      if (!validation.valid) {
        setError(validation.error || "ファイルのバリデーションに失敗しました");
        return;
      }
    }

    setUploadingFiles(filesToUpload.map((file) => ({ file, progress: 0 })));

    const uploadedFiles: UploadedFile[] = [];

    for (let i = 0; i < filesToUpload.length; i++) {
      const file = filesToUpload[i];

      try {
        setUploadingFiles((prev) =>
          prev.map((uf, idx) =>
            idx === i ? { ...uf, progress: 50 } : uf
          )
        );

        const uploaded = await uploadFile({
          userId,
          goalId,
          file,
        });

        uploadedFiles.push(uploaded);

        setUploadingFiles((prev) =>
          prev.map((uf, idx) =>
            idx === i ? { ...uf, progress: 100 } : uf
          )
        );
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "アップロードに失敗しました";
        setUploadingFiles((prev) =>
          prev.map((uf, idx) =>
            idx === i ? { ...uf, error: errorMessage } : uf
          )
        );
      }
    }

    if (uploadedFiles.length > 0) {
      onFilesChange([...files, ...uploadedFiles]);
    }

    setTimeout(() => {
      setUploadingFiles([]);
    }, 1000);
  }, [files, maxFiles, userId, goalId, onFilesChange]);

  const handleRemove = useCallback(async (fileId: string) => {
    try {
      await deleteFile(fileId, userId);
      onFilesChange(files.filter((f) => f.id !== fileId));
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "削除に失敗しました";
      setError(errorMessage);
    }
  }, [files, userId, onFilesChange]);

  const { getRootProps, getInputProps, isDragActive } = useDropzone({
    onDrop: handleUpload,
    accept: Object.fromEntries(
      (ALLOWED_FILE_TYPES as readonly string[]).map((type) => [type, []])
    ),
    maxSize: MAX_FILE_SIZE_MB * 1024 * 1024,
    disabled: disabled || files.length >= maxFiles,
    multiple: true,
  });

  const getFileIcon = (file: UploadedFile | File) => {
    const fileData = "type" in file ? file : { type: (file as File).type };

    if (isImageFile(fileData as UploadedFile)) {
      return <ImageIcon className="w-5 h-5 text-blue-500" />;
    }
    if (isPdfFile(fileData as UploadedFile)) {
      return <FileText className="w-5 h-5 text-red-500" />;
    }
    if (isExcelFile(fileData as UploadedFile) || isCsvFile(fileData as UploadedFile)) {
      return <FileSpreadsheet className="w-5 h-5 text-green-500" />;
    }
    return <File className="w-5 h-5 text-gray-500" />;
  };

  const acceptedFormats = Object.values(FILE_EXTENSIONS).join(", ");

  return (
    <div className="space-y-3">
      <label className="text-sm text-muted-foreground block">
        参考資料を添付（オプション・最大{maxFiles}ファイル）
      </label>

      {/* Dropzone */}
      <div
        {...getRootProps()}
        className={cn(
          "border-2 border-dashed rounded-lg p-6 text-center cursor-pointer transition-all duration-200",
          isDragActive
            ? "border-primary bg-primary/5"
            : "border-muted-foreground/30 hover:border-primary/50 hover:bg-muted/50",
          (disabled || files.length >= maxFiles) && "opacity-50 cursor-not-allowed"
        )}
      >
        <input {...getInputProps()} />
        <Upload className="w-8 h-8 mx-auto mb-2 text-muted-foreground" />
        {isDragActive ? (
          <p className="text-sm text-primary">ここにファイルをドロップ...</p>
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              クリックまたはドラッグ&ドロップでファイルを追加
            </p>
            <p className="text-xs text-muted-foreground/70 mt-1">
              対応形式: {acceptedFormats}（最大{MAX_FILE_SIZE_MB}MB）
            </p>
          </>
        )}
      </div>

      {/* Error Message */}
      {error && (
        <div className="flex items-center gap-2 p-2 rounded-lg bg-destructive/10 border border-destructive/20 text-destructive text-sm">
          <AlertCircle className="w-4 h-4 flex-shrink-0" />
          <span>{error}</span>
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto h-6 px-2"
            onClick={() => setError(null)}
          >
            <X className="w-3 h-3" />
          </Button>
        </div>
      )}

      {/* Uploading Files */}
      {uploadingFiles.length > 0 && (
        <div className="space-y-2">
          {uploadingFiles.map((uf, index) => (
            <Card key={index} className="p-3">
              <div className="flex items-center gap-3">
                <Loader2 className="w-5 h-5 animate-spin text-primary" />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate">{uf.file.name}</p>
                  {uf.error ? (
                    <p className="text-xs text-destructive">{uf.error}</p>
                  ) : (
                    <div className="w-full h-1 bg-muted rounded-full mt-1">
                      <div
                        className="h-full bg-primary rounded-full transition-all duration-300"
                        style={{ width: `${uf.progress}%` }}
                      />
                    </div>
                  )}
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Uploaded Files List */}
      {files.length > 0 && (
        <div className="space-y-2">
          {files.map((file) => (
            <Card key={file.id} className="p-3">
              <div className="flex items-center gap-3">
                {getFileIcon(file)}
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate">{file.name}</p>
                  <p className="text-xs text-muted-foreground">
                    {FILE_EXTENSIONS[file.type] || file.type} • {formatFileSize(file.size)}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive"
                  onClick={() => handleRemove(file.id)}
                  disabled={disabled}
                >
                  <X className="w-4 h-4" />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* File Count Indicator */}
      {files.length > 0 && (
        <p className="text-xs text-muted-foreground text-right">
          {files.length} / {maxFiles} ファイル
        </p>
      )}
    </div>
  );
}
