import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import MetricsBar from './MetricsBar';
import { appStore } from '@/lib/store';

describe('MetricsBar', () => {
  it('renders metric tiles with formatted values', () => {
    appStore.setState({
      metrics: {
        active_agents: 3,
        total_tokens: 12345,
        total_spend_usd: 12.34,
        live_tps: 56.7,
        live_spend_per_s: 0.12,
        completion_rate: 0.42,
      },
    });

    render(<MetricsBar />);
    expect(screen.getByText('Active Agents')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('Total Tokens')).toBeInTheDocument();
    expect(screen.getByText('12,345')).toBeInTheDocument();
    expect(screen.getByText('Total Spend')).toBeInTheDocument();
    expect(screen.getByText('$12.34')).toBeInTheDocument();
    expect(screen.getByText('Live TPS')).toBeInTheDocument();
    expect(screen.getByText('56.7')).toBeInTheDocument();
    expect(screen.getByText('Completion')).toBeInTheDocument();
    expect(screen.getByText('42%')).toBeInTheDocument();
  });
});

