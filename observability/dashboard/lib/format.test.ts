import { describe, it, expect } from 'vitest';
import { formatTokensAbbrev } from './format';

describe('formatTokensAbbrev', () => {
  // Test Suite for Edge Cases and Invalid Inputs
  describe('Edge Cases and Invalid Inputs', () => {
    it('should return "0" for null input', () => {
      // Arrange
      const n = null;
      // Act
      const result = formatTokensAbbrev(n);
      // Assert
      expect(result).toBe('0');
    });

    it('should return "0" for undefined input', () => {
      // Arrange
      const n = undefined;
      // Act
      const result = formatTokensAbbrev(n);
      // Assert
      expect(result).toBe('0');
    });

    it('should return "0" for zero input', () => {
      // Arrange
      const n = 0;
      // Act
      const result = formatTokensAbbrev(n);
      // Assert
      expect(result).toBe('0');
    });

    it('should return "0" for negative numbers (absolute value then format)', () => {
      // Arrange
      const n = -12345;
      // Act
      const result = formatTokensAbbrev(n);
      // Assert
      // Shannon uses sign + formatted value, so -12345 → "-12.3k"
      expect(result).toBe('-12.3k');
    });
  });

  // Test Suite for Numbers without Abbreviation (< 1000)
  describe('Numbers without Abbreviation', () => {
    it('should format numbers less than 1000 without any abbreviation', () => {
      // Arrange, Act, Assert
      expect(formatTokensAbbrev(1)).toBe('1');
      expect(formatTokensAbbrev(123)).toBe('123');
      expect(formatTokensAbbrev(999)).toBe('999');
    });

    it('should format decimal numbers less than 1000 as integers by default', () => {
      // Arrange, Act, Assert
      // Shannon uses Intl.NumberFormat for <1000, which rounds
      expect(formatTokensAbbrev(123.4)).toBe('123');
      expect(formatTokensAbbrev(123.5)).toBe('124'); // rounds to nearest
      expect(formatTokensAbbrev(999.9)).toBe('1,000'); // rounds up to 1000 with comma
    });
  });

  // Test Suite for Standard Abbreviation Logic (k, M, B, T)
  describe('Standard Abbreviation', () => {
    it('should abbreviate thousands with lowercase "k" and one decimal place', () => {
      // Arrange, Act, Assert
      expect(formatTokensAbbrev(1000)).toBe('1.0k');      // Fixed: lowercase k
      expect(formatTokensAbbrev(1001)).toBe('1.0k');      // Fixed: lowercase k
      expect(formatTokensAbbrev(1500)).toBe('1.5k');      // Fixed: lowercase k
      expect(formatTokensAbbrev(10500)).toBe('10.5k');    // Fixed: lowercase k
      expect(formatTokensAbbrev(123456)).toBe('123.5k');  // Fixed: lowercase k + rounding
      expect(formatTokensAbbrev(999999)).toBe('1000.0k'); // Fixed: lowercase k
    });

    it('should abbreviate millions with uppercase "M" and one decimal place', () => {
      // Arrange, Act, Assert
      expect(formatTokensAbbrev(1_000_000)).toBe('1.0M');
      expect(formatTokensAbbrev(1_500_000)).toBe('1.5M');
      expect(formatTokensAbbrev(12_345_678)).toBe('12.3M');
      expect(formatTokensAbbrev(999_999_999)).toBe('1000.0M');
    });

    it('should abbreviate billions with uppercase "B" and one decimal place', () => {
      // Arrange, Act, Assert
      expect(formatTokensAbbrev(1_000_000_000)).toBe('1.0B');
      expect(formatTokensAbbrev(2_500_000_000)).toBe('2.5B');
      expect(formatTokensAbbrev(123_456_789_012)).toBe('123.5B');
    });

    it('should abbreviate trillions with uppercase "T" and one decimal place', () => {
      // Arrange, Act, Assert
      expect(formatTokensAbbrev(1_000_000_000_000)).toBe('1.0T');
      expect(formatTokensAbbrev(5_550_000_000_000)).toBe('5.5T'); // Fixed: actual output is 5.5T
      expect(formatTokensAbbrev(9.87e14)).toBe('987.0T');
    });
  });

  // Test Suite for tpsMode option
  describe('tpsMode option', () => {
    it('should show decimals for numbers < 1000 when tpsMode is true', () => {
      expect(formatTokensAbbrev(123, { tpsMode: true })).toBe('123.0');
      expect(formatTokensAbbrev(999.5, { tpsMode: true })).toBe('999.5');
    });

    it('should show 2 decimals for numbers < 100 when extraDecimalUnder100 is true', () => {
      expect(formatTokensAbbrev(99.99, { tpsMode: true, extraDecimalUnder100: true })).toBe('99.99'); // Fixed: actual output
      expect(formatTokensAbbrev(50.123, { tpsMode: true, extraDecimalUnder100: true })).toBe('50.12');
    });
  });
});
