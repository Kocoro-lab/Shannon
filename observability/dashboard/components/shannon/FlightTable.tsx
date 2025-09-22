'use client';

import { useFlights } from '../../shannon/dashboardContext';

function formatTime(ts: number): string {
  const date = new Date(ts);
  return date.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function statusTone(status: string) {
  switch (status) {
    case 'active':
      return 'text-[#c79224]';
    case 'completed':
      return 'text-[#2ec27e]';
    case 'error':
      return 'text-[#f87171]';
    default:
      return 'text-[#8aa9cf]';
  }
}

export function FlightTable() {
  const flights = useFlights();

  return (
    <div className="h-full min-h-0 flex flex-col border border-[#352b19] bg-black">
      <div className="ui-label ui-label--tab">Task Flights</div>
      <div className="flex-1 min-h-0 overflow-auto no-scrollbar">
        <table
          className="min-w-full text-xs"
          style={{
            tableLayout: 'fixed',
            borderCollapse: 'separate',
            borderSpacing: '2px',
            backgroundColor: '#000',
          }}
        >
          <thead className="text-[#d79326]">
            <tr>
              <th className="text-left px-2 py-1">ID</th>
              <th className="text-left px-2 py-1">Sector</th>
              <th className="text-left px-2 py-1">Status</th>
              <th className="text-left px-2 py-1">Message</th>
              <th className="text-left px-2 py-1">Updated</th>
            </tr>
          </thead>
          <tbody>
            {flights.map((flight) => {
              const rowClass =
                flight.status === 'completed'
                  ? 'tr-status-done'
                  : flight.status === 'active'
                  ? 'tr-status-inprogress'
                  : flight.status === 'error'
                  ? 'bg-[#2d0b0b] text-[#f87171]'
                  : 'tr-status-queued';
              return (
                <tr key={flight.id} className={rowClass}>
                  <td className="px-2 py-1 font-mono">{flight.id}</td>
                  <td className="px-2 py-1">{flight.sector}</td>
                  <td className={`px-2 py-1 uppercase ${statusTone(flight.status)}`}>{flight.status}</td>
                  <td className="px-2 py-1 truncate" title={flight.lastMessage}>
                    {flight.lastMessage || '—'}
                  </td>
                  <td className="px-2 py-1 font-mono text-[#a0a0a0]">{formatTime(flight.lastUpdated)}</td>
                </tr>
              );
            })}
            {flights.length === 0 && (
              <tr>
                <td colSpan={5} className="px-3 py-4 text-center text-[#666]">
                  No agent flights yet. Submit a task to start the traffic flow.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
