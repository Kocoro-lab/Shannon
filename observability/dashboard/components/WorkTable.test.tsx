import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import WorkTable from './WorkTable';
import { appStore } from '@/lib/store';

beforeEach(() => {
  appStore.setState({
    items: {
      A1: { id: 'A1', group: 'A', sector: 'Planning', depends_on: [], estimate_ms: 10000, tps_min: 1, tps_max: 2, tps: 1.5, tokens_done: 12, est_tokens: 15, eta_ms: 8000, status: 'queued' },
      B1: { id: 'B1', group: 'B', sector: 'Build', depends_on: ['A1','X1','X2','X3'], estimate_ms: 10000, tps_min: 1, tps_max: 2, tps: 1.2, tokens_done: 3, est_tokens: 15, eta_ms: 6000, status: 'assigned' },
      C1: { id: 'C1', group: 'C', sector: 'Eval', depends_on: ['A1'], estimate_ms: 5000, tps_min: 1, tps_max: 2, tps: 1.7, tokens_done: 5, est_tokens: 8, eta_ms: 2000, status: 'in_progress', agent_id: 'AG1', started_at: Date.now() - 3000 },
    },
  });
});

describe('WorkTable', () => {
  it('renders rows sorted by status then id and truncates deps', () => {
    render(<WorkTable />);
    const rows = screen.getAllByRole('row');
    // rows[0] is header
    const dataRows = rows.slice(1);
    const first = within(dataRows[0]).getByText('A1');
    expect(first).toBeInTheDocument();
    // Check truncation text and new columns render
    expect(screen.getByText(/\+2 more/)).toBeInTheDocument();
    expect(screen.getByText(/Tokens/i)).toBeInTheDocument();
    expect(screen.getByText(/TPS/i)).toBeInTheDocument();
    expect(screen.getByText(/ETA/i)).toBeInTheDocument();
    // A simple spot check for formatted values
    expect(screen.getByText(/12\s*\/\s*15/)).toBeInTheDocument();
  });
});
