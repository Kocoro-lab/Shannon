"use client";

import { useState, useRef, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Pencil, Check, X } from "lucide-react";
import { cn } from "@/lib/utils";

interface EditableFieldProps {
  value: string | number;
  onSave: (newValue: string | number) => void;
  type?: "text" | "number" | "textarea" | "select";
  options?: { value: string; label: string }[];
  className?: string;
  displayClassName?: string;
  inputClassName?: string;
  placeholder?: string;
  label?: string;
  disabled?: boolean;
}

export function EditableField({
  value,
  onSave,
  type = "text",
  options = [],
  className,
  displayClassName,
  inputClassName,
  placeholder,
  label,
  disabled = false,
}: EditableFieldProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [editValue, setEditValue] = useState(String(value));
  const inputRef = useRef<HTMLInputElement | HTMLTextAreaElement>(null);

  useEffect(() => {
    setEditValue(String(value));
  }, [value]);

  useEffect(() => {
    if (isEditing && inputRef.current) {
      inputRef.current.focus();
      if (inputRef.current instanceof HTMLInputElement) {
        inputRef.current.select();
      }
    }
  }, [isEditing]);

  const handleSave = () => {
    const newValue = type === "number" ? Number(editValue) : editValue;
    onSave(newValue);
    setIsEditing(false);
  };

  const handleCancel = () => {
    setEditValue(String(value));
    setIsEditing(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && type !== "textarea") {
      handleSave();
    } else if (e.key === "Escape") {
      handleCancel();
    }
  };

  if (disabled) {
    return (
      <div className={cn("", className)}>
        {label && (
          <span className="text-xs text-muted-foreground block mb-1">
            {label}
          </span>
        )}
        <span className={displayClassName}>{value}</span>
      </div>
    );
  }

  if (isEditing) {
    if (type === "select" && options.length > 0) {
      return (
        <div className={cn("flex items-center gap-2", className)}>
          <Select value={editValue} onValueChange={setEditValue}>
            <SelectTrigger className={cn("w-[180px]", inputClassName)}>
              <SelectValue placeholder={placeholder} />
            </SelectTrigger>
            <SelectContent>
              {options.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button size="icon" variant="ghost" onClick={handleSave}>
            <Check className="w-4 h-4 text-green-600" />
          </Button>
          <Button size="icon" variant="ghost" onClick={handleCancel}>
            <X className="w-4 h-4 text-destructive" />
          </Button>
        </div>
      );
    }

    if (type === "textarea") {
      return (
        <div className={cn("space-y-2", className)}>
          {label && (
            <span className="text-xs text-muted-foreground block">
              {label}
            </span>
          )}
          <textarea
            ref={inputRef as React.RefObject<HTMLTextAreaElement>}
            value={editValue}
            onChange={(e) => setEditValue(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            className={cn(
              "w-full min-h-[100px] p-2 rounded-md border border-input bg-background text-sm focus:outline-none focus:ring-2 focus:ring-ring",
              inputClassName
            )}
          />
          <div className="flex gap-2">
            <Button size="sm" onClick={handleSave}>
              <Check className="w-3 h-3 mr-1" />
              保存
            </Button>
            <Button size="sm" variant="ghost" onClick={handleCancel}>
              <X className="w-3 h-3 mr-1" />
              キャンセル
            </Button>
          </div>
        </div>
      );
    }

    return (
      <div className={cn("flex items-center gap-2", className)}>
        <Input
          ref={inputRef as React.RefObject<HTMLInputElement>}
          type={type}
          value={editValue}
          onChange={(e) => setEditValue(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          className={cn("h-8", inputClassName)}
        />
        <Button size="icon" variant="ghost" onClick={handleSave}>
          <Check className="w-4 h-4 text-green-600" />
        </Button>
        <Button size="icon" variant="ghost" onClick={handleCancel}>
          <X className="w-4 h-4 text-destructive" />
        </Button>
      </div>
    );
  }

  return (
    <div className={cn("group", className)}>
      {label && (
        <span className="text-xs text-muted-foreground block mb-1">
          {label}
        </span>
      )}
      <button
        onClick={() => setIsEditing(true)}
        className="inline-flex items-center gap-2 rounded px-1 -mx-1 hover:bg-muted/50 transition-colors cursor-pointer"
        aria-label="編集"
      >
        <span className={displayClassName}>
          {type === "select" && options.length > 0
            ? options.find((o) => o.value === String(value))?.label || value
            : value}
        </span>
        <Pencil className="w-3 h-3 text-muted-foreground/50 group-hover:text-muted-foreground transition-colors flex-shrink-0" />
      </button>
    </div>
  );
}

interface EditableRowProps {
  label: string;
  value: string | number;
  onSave: (newValue: string | number) => void;
  type?: "text" | "number" | "select";
  options?: { value: string; label: string }[];
  className?: string;
}

export function EditableRow({
  label,
  value,
  onSave,
  type = "text",
  options = [],
  className,
}: EditableRowProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [editValue, setEditValue] = useState(String(value));
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setEditValue(String(value));
  }, [value]);

  useEffect(() => {
    if (isEditing && inputRef.current) {
      inputRef.current.focus();
      inputRef.current.select();
    }
  }, [isEditing]);

  const handleSave = () => {
    const newValue = type === "number" ? Number(editValue) : editValue;
    onSave(newValue);
    setIsEditing(false);
  };

  const handleCancel = () => {
    setEditValue(String(value));
    setIsEditing(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      handleSave();
    } else if (e.key === "Escape") {
      handleCancel();
    }
  };

  const displayValue =
    type === "select" && options.length > 0
      ? options.find((o) => o.value === String(value))?.label || value
      : value;

  if (isEditing) {
    if (type === "select" && options.length > 0) {
      return (
        <div
          className={cn(
            "flex items-center justify-between p-2 rounded-lg bg-neutral-1",
            className
          )}
        >
          <span className="text-sm text-muted-foreground">{label}</span>
          <div className="flex items-center gap-1">
            <Select value={editValue} onValueChange={setEditValue}>
              <SelectTrigger className="h-7 w-[100px] text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {options.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button size="icon" variant="ghost" className="h-6 w-6" onClick={handleSave}>
              <Check className="w-3 h-3 text-green-600" />
            </Button>
            <Button size="icon" variant="ghost" className="h-6 w-6" onClick={handleCancel}>
              <X className="w-3 h-3 text-destructive" />
            </Button>
          </div>
        </div>
      );
    }

    return (
      <div
        className={cn(
          "flex items-center justify-between p-2 rounded-lg bg-neutral-1",
          className
        )}
      >
        <span className="text-sm text-muted-foreground">{label}</span>
        <div className="flex items-center gap-1">
          <Input
            ref={inputRef}
            type={type}
            value={editValue}
            onChange={(e) => setEditValue(e.target.value)}
            onKeyDown={handleKeyDown}
            className="h-7 w-[100px] text-xs"
          />
          <Button size="icon" variant="ghost" className="h-6 w-6" onClick={handleSave}>
            <Check className="w-3 h-3 text-green-600" />
          </Button>
          <Button size="icon" variant="ghost" className="h-6 w-6" onClick={handleCancel}>
            <X className="w-3 h-3 text-destructive" />
          </Button>
        </div>
      </div>
    );
  }

  return (
    <button
      className={cn(
        "group flex items-center justify-between p-2 rounded-lg bg-neutral-1 cursor-pointer hover:bg-neutral-2 transition-colors w-full text-left",
        className
      )}
      onClick={() => setIsEditing(true)}
    >
      <span className="text-sm text-muted-foreground">{label}</span>
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium capitalize">{displayValue}</span>
        <Pencil className="w-3 h-3 text-muted-foreground/50 group-hover:text-muted-foreground transition-colors flex-shrink-0" />
      </div>
    </button>
  );
}
