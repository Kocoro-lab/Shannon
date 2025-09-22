'use client';

import { useEffect, useRef, useState, useMemo } from 'react';
import { useFlights } from '../../shannon/dashboardContext';

interface FlightPosition {
  id: string;
  x: number;
  y: number;
  angle: number;
  distance: number;
  status: string;
  label: string;
  trail: { x: number; y: number }[];
}

export function RadarDisplay() {
  const flights = useFlights();
  const svgRef = useRef<SVGSVGElement>(null);
  const [sweepAngle, setSweepAngle] = useState(0);
  const [dimensions, setDimensions] = useState({ width: 600, height: 600 });
  // Tick to drive simple animation without re-deriving data elsewhere
  const [tick, setTick] = useState(0);

  // Update dimensions on mount and resize
  useEffect(() => {
    const updateDimensions = () => {
      if (svgRef.current?.parentElement) {
        const rect = svgRef.current.parentElement.getBoundingClientRect();
        setDimensions({
          width: rect.width,
          height: rect.height
        });
      }
    };

    updateDimensions();
    window.addEventListener('resize', updateDimensions);
    return () => window.removeEventListener('resize', updateDimensions);
  }, []);

  // Animate radar sweep
  useEffect(() => {
    const interval = setInterval(() => {
      setSweepAngle((prev) => (prev + 2) % 360);
      // Advance animation clock to re-evaluate positions
      setTick((t) => (t + 1) % 1000000);
    }, 50);
    return () => clearInterval(interval);
  }, []);

  // Calculate flight positions
  const flightPositions = useMemo<FlightPosition[]>(() => {
    const centerX = dimensions.width / 2;
    const centerY = dimensions.height / 2;
    const maxRadius = Math.min(centerX, centerY) * 0.9;

    // Tiny hash to create deterministic per-flight variation (0..1)
    const h01 = (id: string) => {
      let h = 2166136261 >>> 0;
      for (let i = 0; i < id.length; i++) {
        h ^= id.charCodeAt(i);
        h = Math.imul(h, 16777619);
      }
      return (h >>> 0) / 4294967295;
    };

    return flights.map((flight, index) => {
      // Distribute flights around the circle
      const baseAngle = (index * 137.5) % 360; // Golden angle for better distribution

      // Calculate distance based on status
      let distance: number;
      switch (flight.status) {
        case 'queued':
          distance = maxRadius * 0.95; // Outer edge
          break;
        case 'active':
          // Animate inward based on how long it's been active
          // If no startedAt, keep near edge.
          const started = flight.startedAt ?? flight.lastUpdated;
          const elapsedMs = Math.max(0, Date.now() - started);
          // Time-to-center tuning: larger = slower approach
          const ACTIVE_TRAVEL_MS = 25000; // ~25s from edge to near center
          const u = Math.max(0, Math.min(1, elapsedMs / ACTIVE_TRAVEL_MS));
          // Easing for nicer motion
          const ease = 0.5 - 0.5 * Math.cos(Math.PI * u);
          // Travel from 95% radius down to 15%
          distance = maxRadius * (0.95 - ease * 0.80);
          break;
        case 'completed':
          distance = maxRadius * 0.1; // Near center
          break;
        case 'error':
          distance = maxRadius * 0.6; // Stop mid-flight
          break;
        default:
          distance = maxRadius * 0.8;
      }

      // Subtle curved approach like original ATC: add a small, deterministic
      // spiral offset that grows with progress.
      const seed = h01(flight.id);
      const dir = seed > 0.5 ? 1 : -1; // clockwise vs counter
      const curveAmt = 0.6;            // 0 straight, 1 curvier
      const progress = flight.status === 'active'
        ? Math.max(0, Math.min(1, 1 - distance / (maxRadius * 0.95)))
        : flight.status === 'completed'
        ? 1
        : 0;
      const maxTurn = 0.4 * Math.PI;   // cap at ~0.4π rad (~72deg)
      const ease = 0.5 - 0.5 * Math.cos(Math.PI * progress);
      const angleOffset = dir * curveAmt * maxTurn * ease;

      // Convert polar to cartesian with curve offset
      const radians = (baseAngle * Math.PI) / 180 + angleOffset;
      const x = centerX + distance * Math.cos(radians);
      const y = centerY + distance * Math.sin(radians);

      // Generate trail for active flights
      const trail: { x: number; y: number }[] = [];
      if (flight.status === 'active') {
        for (let i = 1; i <= 3; i++) {
          const trailDist = distance + (maxRadius * 0.05 * i);
          const trailX = centerX + trailDist * Math.cos(radians);
          const trailY = centerY + trailDist * Math.sin(radians);
          trail.push({ x: trailX, y: trailY });
        }
      }

      return {
        id: flight.id,
        x,
        y,
        angle: baseAngle,
        distance,
        status: flight.status,
        label: flight.agentId.slice(-4), // Short ID for display
        trail
      };
    });
  }, [flights, dimensions, tick]);

  const centerX = dimensions.width / 2;
  const centerY = dimensions.height / 2;
  const maxRadius = Math.min(centerX, centerY) * 0.9;

  // Radar sweep coordinates
  const sweepRadians = (sweepAngle * Math.PI) / 180;
  const sweepX = centerX + maxRadius * Math.cos(sweepRadians);
  const sweepY = centerY + maxRadius * Math.sin(sweepRadians);

  return (
    <div className="h-full min-h-0 border border-[#352b19] bg-black">
      <div className="ui-label ui-label--tab">Task Flights Radar</div>
      {/* Ensure the radar has a real rendering area; without a minimum height,
         the parent grid can collapse and the SVG measures 0x0, appearing blank. */}
      <div className="flex-1 relative min-h-[320px]">
        <svg
          ref={svgRef}
          width="100%"
          height="100%"
          viewBox={`0 0 ${dimensions.width} ${dimensions.height}`}
          className="absolute inset-0"
          style={{ background: 'radial-gradient(circle, #0a0f0a 0%, #000000 100%)' }}
        >
          {/* Radar circles */}
          {[0.25, 0.5, 0.75, 1].map((ratio) => (
            <circle
              key={ratio}
              cx={centerX}
              cy={centerY}
              r={maxRadius * ratio}
              fill="none"
              stroke="#1b1407"
              strokeWidth="1"
              opacity="0.5"
            />
          ))}

          {/* Cross-hair lines */}
          <line x1={centerX} y1={centerY - maxRadius} x2={centerX} y2={centerY + maxRadius} stroke="#1b1407" strokeWidth="1" opacity="0.3" />
          <line x1={centerX - maxRadius} y1={centerY} x2={centerX + maxRadius} y2={centerY} stroke="#1b1407" strokeWidth="1" opacity="0.3" />

          {/* Diagonal lines */}
          {[45, 135, 225, 315].map((angle) => {
            const rad = (angle * Math.PI) / 180;
            const x = maxRadius * Math.cos(rad);
            const y = maxRadius * Math.sin(rad);
            return (
              <line
                key={angle}
                x1={centerX - x}
                y1={centerY - y}
                x2={centerX + x}
                y2={centerY + y}
                stroke="#1b1407"
                strokeWidth="1"
                opacity="0.2"
              />
            );
          })}

          {/* Radar sweep with fade effect */}
          <defs>
            <linearGradient id="sweepGradient" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor="#2ec27e" stopOpacity="0" />
              <stop offset="50%" stopColor="#2ec27e" stopOpacity="0.3" />
              <stop offset="100%" stopColor="#2ec27e" stopOpacity="0.6" />
            </linearGradient>
          </defs>

          <line
            x1={centerX}
            y1={centerY}
            x2={sweepX}
            y2={sweepY}
            stroke="url(#sweepGradient)"
            strokeWidth="2"
          />

          {/* Flight trails */}
          {flightPositions.map((flight) =>
            flight.trail.map((point, i) => (
              <circle
                key={`${flight.id}-trail-${i}`}
                cx={point.x}
                cy={point.y}
                r="1"
                fill="#2ec27e"
                opacity={0.3 - i * 0.1}
              />
            ))
          )}

          {/* Flights */}
          {flightPositions.map((flight) => {
            const color =
              flight.status === 'error' ? '#f87171' :
              flight.status === 'active' ? '#c79224' :
              flight.status === 'completed' ? '#2ec27e' :
              '#8aa9cf';

            const isBlinking = flight.status === 'error';

            return (
              <g key={flight.id}>
                {/* Flight icon - triangle pointing towards center */}
                <g transform={`translate(${flight.x}, ${flight.y})`}>
                  <g transform={`rotate(${flight.angle + 180} 0 0)`}>
                    <path
                      d="M -4,-4 L 4,-4 L 0,6 Z"
                      fill={color}
                      className={isBlinking ? 'animate-pulse' : ''}
                    />
                  </g>
                  {/* Flight label */}
                  <text
                    x="8"
                    y="3"
                    fill={color}
                    fontSize="9"
                    fontFamily="monospace"
                  >
                    {flight.label}
                  </text>
                </g>
              </g>
            );
          })}

          {/* Center point */}
          <circle cx={centerX} cy={centerY} r="3" fill="#2ec27e" opacity="0.8" />
          <circle cx={centerX} cy={centerY} r="5" fill="none" stroke="#2ec27e" strokeWidth="1" opacity="0.5" />

          {/* Range labels */}
          <text x={centerX + 5} y={centerY - maxRadius * 0.25 + 3} fill="#626262" fontSize="8" fontFamily="monospace">75</text>
          <text x={centerX + 5} y={centerY - maxRadius * 0.5 + 3} fill="#626262" fontSize="8" fontFamily="monospace">50</text>
          <text x={centerX + 5} y={centerY - maxRadius * 0.75 + 3} fill="#626262" fontSize="8" fontFamily="monospace">25</text>
        </svg>

        {/* Status legend */}
        <div className="absolute bottom-2 left-2 text-[9px] space-y-1 font-mono">
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 bg-[#8aa9cf]"></div>
            <span className="text-[#626262]">QUEUED</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 bg-[#c79224]"></div>
            <span className="text-[#626262]">ACTIVE</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 bg-[#2ec27e]"></div>
            <span className="text-[#626262]">COMPLETED</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 bg-[#f87171] animate-pulse"></div>
            <span className="text-[#626262]">ERROR</span>
          </div>
        </div>
      </div>
    </div>
  );
}
