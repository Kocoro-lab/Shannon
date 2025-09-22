import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import Home from './page';

describe('renders-root', () => {
  it('renders the root page and shows sentinel text', () => {
    render(<Home />);
    
    const heading = screen.getByText('Calming Control Room');
    expect(heading).toBeInTheDocument();
  });
});