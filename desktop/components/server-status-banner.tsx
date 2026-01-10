"use client";

import React from 'react';
import { AlertCircle, Loader2, Server, WifiOff } from 'lucide-react';
import { useServer } from '@/lib/server-context';

export function ServerStatusBanner() {
  const { status, error, isTauri } = useServer();

  // Don't show banner if server is ready or in web mode (non-Tauri)
  if (status === 'ready' || !isTauri) {
    return null;
  }

  const getStatusInfo = () => {
    switch (status) {
      case 'initializing':
      case 'starting':
        return {
          icon: <Loader2 className="h-4 w-4 animate-spin" />,
          message: 'Starting server...',
          bgColor: 'bg-blue-500/10',
          textColor: 'text-blue-600 dark:text-blue-400',
          borderColor: 'border-blue-500/20',
        };
      case 'failed':
        return {
          icon: <AlertCircle className="h-4 w-4" />,
          message: error || 'Server failed to start',
          bgColor: 'bg-red-500/10',
          textColor: 'text-red-600 dark:text-red-400',
          borderColor: 'border-red-500/20',
        };
      case 'unknown':
        return {
          icon: <WifiOff className="h-4 w-4" />,
          message: 'Server unavailable',
          bgColor: 'bg-yellow-500/10',
          textColor: 'text-yellow-600 dark:text-yellow-400',
          borderColor: 'border-yellow-500/20',
        };
      default:
        return {
          icon: <Server className="h-4 w-4" />,
          message: 'Checking server status...',
          bgColor: 'bg-gray-500/10',
          textColor: 'text-gray-600 dark:text-gray-400',
          borderColor: 'border-gray-500/20',
        };
    }
  };

  const statusInfo = getStatusInfo();

  return (
    <div className={`w-full border-b ${statusInfo.borderColor} ${statusInfo.bgColor}`}>
      <div className="container mx-auto px-4 py-2">
        <div className={`flex items-center gap-2 text-sm ${statusInfo.textColor}`}>
          {statusInfo.icon}
          <span className="font-medium">{statusInfo.message}</span>
        </div>
      </div>
    </div>
  );
}
