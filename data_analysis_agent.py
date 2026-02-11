#!/usr/bin/env python3
"""
Online Tourism Data Analysis Agent

This script creates a data analysis agent that processes online tourism data
and generates structured reports according to a template.
"""

import os
import pandas as pd
import numpy as np
from datetime import datetime
from typing import Dict, Any, List
import json
import yaml
from dataclasses import dataclass
from pathlib import Path


@dataclass
class AnalysisReport:
    """Structure for the analysis report"""
    overview: Dict[str, Any]
    trends: List[Dict[str, Any]]
    insights: List[Dict[str, Any]]
    recommendations: List[Dict[str, Any]]
    data_quality: Dict[str, Any]


class TourismDataAnalyzer:
    """Main class for analyzing tourism data"""
    
    def __init__(self):
        self.data = None
        self.report_template = self._load_report_template()
    
    def _load_report_template(self) -> Dict[str, Any]:
        """Load the report template"""
        template = {
            "title": "Online Tourism Data Analysis Report",
            "date_generated": datetime.now().isoformat(),
            "sections": {
                "overview": {
                    "summary_statistics": {},
                    "data_period": {},
                    "key_metrics": {}
                },
                "trends": [],
                "insights": [],
                "recommendations": [],
                "data_quality": {
                    "completeness": {},
                    "consistency_issues": [],
                    "anomalies": []
                }
            }
        }
        return template
    
    def load_data(self, file_path: str) -> bool:
        """Load tourism data from various file formats"""
        try:
            file_ext = Path(file_path).suffix.lower()
            
            if file_ext == '.csv':
                self.data = pd.read_csv(file_path)
            elif file_ext in ['.xlsx', '.xls']:
                self.data = pd.read_excel(file_path)
            elif file_ext == '.json':
                with open(file_path, 'r') as f:
                    json_data = json.load(f)
                self.data = pd.DataFrame(json_data)
            else:
                raise ValueError(f"Unsupported file format: {file_ext}")
                
            print(f"Successfully loaded data with shape: {self.data.shape}")
            return True
            
        except Exception as e:
            print(f"Error loading data: {str(e)}")
            return False
    
    def validate_data(self) -> Dict[str, Any]:
        """Validate the tourism data structure and content"""
        validation_results = {
            "has_required_columns": True,
            "missing_values": {},
            "data_types": {},
            "date_range": {},
            "duplicates": 0
        }
        
        if self.data is None:
            return validation_results
        
        # Check for common tourism data columns
        required_cols = [
            'booking_date', 'travel_date', 'destination', 
            'price', 'customer_id', 'booking_source'
        ]
        
        missing_cols = []
        for col in required_cols:
            if col not in self.data.columns:
                missing_cols.append(col)
        
        validation_results["has_required_columns"] = len(missing_cols) == 0
        validation_results["missing_columns"] = missing_cols
        
        # Count missing values per column
        for col in self.data.columns:
            missing_count = self.data[col].isnull().sum()
            if missing_count > 0:
                validation_results["missing_values"][col] = missing_count
        
        # Check data types
        for col in self.data.columns:
            validation_results["data_types"][col] = str(self.data[col].dtype)
        
        # Find duplicates
        validation_results["duplicates"] = self.data.duplicated().sum()
        
        return validation_results
    
    def generate_summary_statistics(self) -> Dict[str, Any]:
        """Generate basic summary statistics"""
        if self.data is None:
            return {}
        
        stats = {
            "total_records": len(self.data),
            "unique_destinations": self.data['destination'].nunique() if 'destination' in self.data.columns else 0,
            "avg_price": float(self.data['price'].mean()) if 'price' in self.data.columns else 0,
            "min_price": float(self.data['price'].min()) if 'price' in self.data.columns else 0,
            "max_price": float(self.data['price'].max()) if 'price' in self.data.columns else 0,
            "total_revenue": float(self.data['price'].sum()) if 'price' in self.data.columns else 0,
        }
        
        # Add date range if dates are available
        if 'booking_date' in self.data.columns:
            booking_dates = pd.to_datetime(self.data['booking_date'], errors='coerce')
            valid_dates = booking_dates.dropna()
            if len(valid_dates) > 0:
                stats["date_range"] = {
                    "start": valid_dates.min().isoformat(),
                    "end": valid_dates.max().isoformat()
                }
        
        return stats
    
    def analyze_trends(self) -> List[Dict[str, Any]]:
        """Analyze trends in the tourism data"""
        trends = []
        
        if self.data is None:
            return trends
        
        # Convert date columns if they exist
        if 'booking_date' in self.data.columns:
            self.data['booking_date'] = pd.to_datetime(self.data['booking_date'], errors='coerce')
        
        if 'travel_date' in self.data.columns:
            self.data['travel_date'] = pd.to_datetime(self.data['travel_date'], errors='coerce')
        
        # Booking trends over time
        if 'booking_date' in self.data.columns:
            monthly_bookings = self.data.groupby(
                self.data['booking_date'].dt.to_period('M')
            ).size().reset_index(name='count')
            
            if len(monthly_bookings) > 0:
                trend_data = {
                    "metric": "Monthly Bookings",
                    "time_series": [
                        {"period": str(row['booking_date']), "value": int(row['count'])}
                        for _, row in monthly_bookings.iterrows()
                    ],
                    "direction": "increasing" if len(monthly_bookings) > 1 and 
                                monthly_bookings.iloc[-1]['count'] > monthly_bookings.iloc[0]['count'] else "decreasing"
                }
                trends.append(trend_data)
        
        # Price trends by destination
        if 'destination' in self.data.columns and 'price' in self.data.columns:
            avg_prices_by_destination = (
                self.data.groupby('destination')['price']
                .mean()
                .sort_values(ascending=False)
                .head(10)
                .round(2)
            )
            
            price_trend = {
                "metric": "Top Destinations by Average Price",
                "data": [
                    {"destination": dest, "avg_price": float(price)}
                    for dest, price in avg_prices_by_destination.items()
                ]
            }
            trends.append(price_trend)
        
        # Booking source distribution
        if 'booking_source' in self.data.columns:
            source_distribution = self.data['booking_source'].value_counts()
            
            source_trend = {
                "metric": "Booking Source Distribution",
                "data": [
                    {"source": source, "count": int(count)}
                    for source, count in source_distribution.items()
                ]
            }
            trends.append(source_trend)
        
        return trends
    
    def generate_insights(self) -> List[Dict[str, Any]]:
        """Generate insights from the data"""
        insights = []
        
        if self.data is None:
            return insights
        
        # Revenue insights
        if 'price' in self.data.columns:
            total_revenue = self.data['price'].sum()
            avg_revenue_per_booking = self.data['price'].mean()
            
            insights.append({
                "category": "Revenue",
                "description": f"Total revenue: ${total_revenue:,.2f}",
                "significance": "high"
            })
            
            insights.append({
                "category": "Revenue",
                "description": f"Average revenue per booking: ${avg_revenue_per_booking:.2f}",
                "significance": "medium"
            })
        
        # Destination popularity
        if 'destination' in self.data.columns:
            top_destinations = self.data['destination'].value_counts().head(5)
            
            for i, (dest, count) in enumerate(top_destinations.items()):
                insights.append({
                    "category": "Destination Popularity",
                    "description": f"{dest} is the #{i+1} most popular destination with {count} bookings",
                    "significance": "high" if i == 0 else "medium"
                })
        
        # Seasonal patterns (if dates available)
        if 'travel_date' in self.data.columns:
            self.data['travel_month'] = self.data['travel_date'].dt.month
            monthly_bookings = self.data['travel_month'].value_counts().sort_index()
            
            peak_month = monthly_bookings.idxmax()
            low_month = monthly_bookings.idxmin()
            
            insights.append({
                "category": "Seasonality",
                "description": f"Peak travel month is {peak_month}, lowest is {low_month}",
                "significance": "high"
            })
        
        return insights
    
    def generate_recommendations(self) -> List[Dict[str, Any]]:
        """Generate recommendations based on the analysis"""
        recommendations = []
        
        if self.data is None:
            return recommendations
        
        # Pricing recommendations
        if 'price' in self.data.columns:
            price_stats = self.data['price'].describe()
            
            if price_stats['std'] / price_stats['mean'] > 0.5:  # High variance
                recommendations.append({
                    "category": "Pricing",
                    "description": "High price variance detected. Consider implementing dynamic pricing strategies.",
                    "priority": "high"
                })
        
        # Destination recommendations
        if 'destination' in self.data.columns:
            dest_counts = self.data['destination'].value_counts()
            top_3 = dest_counts.head(3)
            bottom_3 = dest_counts.tail(3)
            
            recommendations.append({
                "category": "Destination Strategy",
                "description": f"Focus marketing on top destinations: {', '.join(top_3.index.tolist())}",
                "priority": "medium"
            })
            
            recommendations.append({
                "category": "Destination Strategy",
                "description": f"Consider promotional campaigns for underperforming destinations: {', '.join(bottom_3.index.tolist())}",
                "priority": "low"
            })
        
        # Booking source optimization
        if 'booking_source' in self.data.columns:
            source_counts = self.data['booking_source'].value_counts()
            top_source = source_counts.index[0]
            bottom_source = source_counts.index[-1]
            
            recommendations.append({
                "category": "Channel Optimization",
                "description": f"Maximize investment in {top_source} channel as it drives most bookings",
                "priority": "high"
            })
            
            recommendations.append({
                "category": "Channel Optimization",
                "description": f"Evaluate effectiveness of {bottom_source} channel",
                "priority": "medium"
            })
        
        return recommendations
    
    def analyze_data_quality(self) -> Dict[str, Any]:
        """Analyze data quality"""
        quality_report = {
            "completeness": {},
            "consistency_issues": [],
            "anomalies": []
        }
        
        if self.data is None:
            return quality_report
        
        # Completeness analysis
        total_cells = self.data.size
        null_cells = self.data.isnull().sum().sum()
        completeness_percentage = ((total_cells - null_cells) / total_cells) * 100
        
        quality_report["completeness"] = {
            "overall_completeness": round(completeness_percentage, 2),
            "total_records": len(self.data),
            "total_attributes": len(self.data.columns),
            "null_cells": int(null_cells),
            "total_cells": int(total_cells)
        }
        
        # Column-wise completeness
        for col in self.data.columns:
            col_total = len(self.data)
            col_nulls = self.data[col].isnull().sum()
            col_completeness = ((col_total - col_nulls) / col_total) * 100
            quality_report["completeness"][col] = round(col_completeness, 2)
        
        # Consistency checks
        if 'price' in self.data.columns:
            negative_prices = self.data[self.data['price'] < 0]
            if len(negative_prices) > 0:
                quality_report["consistency_issues"].append({
                    "issue": "Negative prices detected",
                    "count": len(negative_prices),
                    "sample_rows": negative_prices.index[:5].tolist()
                })
        
        # Anomaly detection for numeric columns
        for col in self.data.select_dtypes(include=[np.number]).columns:
            Q1 = self.data[col].quantile(0.25)
            Q3 = self.data[col].quantile(0.75)
            IQR = Q3 - Q1
            lower_bound = Q1 - 1.5 * IQR
            upper_bound = Q3 + 1.5 * IQR
            
            outliers = self.data[(self.data[col] < lower_bound) | (self.data[col] > upper_bound)]
            if len(outliers) > 0:
                quality_report["anomalies"].append({
                    "column": col,
                    "outlier_count": len(outliers),
                    "percentage": round((len(outliers) / len(self.data)) * 100, 2)
                })
        
        return quality_report
    
    def generate_report(self) -> AnalysisReport:
        """Generate the complete analysis report"""
        if self.data is None:
            raise ValueError("No data loaded. Call load_data() first.")
        
        report = AnalysisReport(
            overview=self.generate_summary_statistics(),
            trends=self.analyze_trends(),
            insights=self.generate_insights(),
            recommendations=self.generate_recommendations(),
            data_quality=self.analyze_data_quality()
        )
        
        return report
    
    def save_report(self, report: AnalysisReport, output_path: str):
        """Save the report to a file"""
        report_dict = {
            "title": "Online Tourism Data Analysis Report",
            "date_generated": datetime.now().isoformat(),
            "overview": report.overview,
            "trends": report.trends,
            "insights": report.insights,
            "recommendations": report.recommendations,
            "data_quality": report.data_quality
        }
        
        with open(output_path, 'w', encoding='utf-8') as f:
            json.dump(report_dict, f, indent=2, ensure_ascii=False, default=str)
        
        print(f"Report saved to {output_path}")


def main():
    """Example usage of the Tourism Data Analyzer"""
    analyzer = TourismDataAnalyzer()
    
    # Example: Load sample data
    # This would typically be replaced with actual file path from user input
    sample_data_path = "sample_tourism_data.csv"
    
    # Create a sample dataset if it doesn't exist
    if not os.path.exists(sample_data_path):
        print("Creating sample tourism data...")
        sample_data = {
            'booking_date': pd.date_range(start='2023-01-01', periods=1000, freq='D'),
            'travel_date': pd.date_range(start='2023-02-01', periods=1000, freq='D'),
            'destination': np.random.choice(['Paris', 'Tokyo', 'New York', 'London', 'Sydney'], 1000),
            'price': np.random.uniform(500, 3000, 1000),
            'customer_id': range(1, 1001),
            'booking_source': np.random.choice(['Website', 'Mobile App', 'Travel Agency'], 1000)
        }
        
        df = pd.DataFrame(sample_data)
        df.to_csv(sample_data_path, index=False)
        print(f"Sample data created: {sample_data_path}")
    
    # Load the data
    if analyzer.load_data(sample_data_path):
        print("Validating data...")
        validation = analyzer.validate_data()
        print(f"Validation results: {validation}")
        
        print("Generating analysis report...")
        report = analyzer.generate_report()
        
        output_path = "tourism_analysis_report.json"
        analyzer.save_report(report, output_path)
        print(f"Analysis complete! Report saved to {output_path}")


if __name__ == "__main__":
    main()