import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { CheckCircle2, Code, Zap, Shield, Globe, Database } from 'lucide-react'

export const Route = createFileRoute('/')({
  component: LandingPage,
})

function LandingPage() {
  const navigate = useNavigate()
  const token = localStorage.getItem('authToken')
  
  // If logged in, redirect to dashboard
  useEffect(() => {
    if (token) {
      navigate({ to: '/dashboard' })
    }
  }, [token, navigate])

  return (
    <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white">
      {/* Navigation */}
      <nav className="bg-white shadow-sm border-b">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between h-16">
            <div className="flex items-center">
              <h1 className="text-2xl font-bold bg-gradient-to-r from-blue-600 to-purple-600 bg-clip-text text-transparent">
                🌍 GeoCode API
              </h1>
            </div>
            <div className="flex items-center space-x-4">
              <a href="/docs" target="_blank" rel="noopener noreferrer" className="text-gray-600 hover:text-blue-600 px-3 py-2 rounded-md text-sm font-medium">
                Documentation
              </a>
              <Link to="/auth/signin">
                <Button variant="outline">Sign In</Button>
              </Link>
              <Link to="/auth/signup">
                <Button>Get Started</Button>
              </Link>
            </div>
          </div>
        </div>
      </nav>

      {/* Hero Section */}
      <div className="bg-gradient-to-r from-blue-600 to-purple-600 text-white">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-24">
          <div className="text-center">
            <h1 className="text-4xl md:text-6xl font-bold mb-6">
              Professional ZIP Code Data API
            </h1>
            <p className="text-xl md:text-2xl mb-8 text-blue-100 max-w-3xl mx-auto">
              Get accurate US ZIP code data, calculate distances, and power your location-based applications with our fast, reliable API.
            </p>
            <div className="flex flex-col sm:flex-row gap-4 justify-center">
              <Link to="/auth/signup">
                <Button size="lg">
                  Start Free Trial
                </Button>
              </Link>
              <a href="/docs" target="_blank" rel="noopener noreferrer">
                <Button size="lg" variant="secondary" >
                  View Documentation
                </Button>
              </a>
            </div>
          </div>
        </div>
      </div>

      {/* Features */}
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-24">
        <div className="text-center mb-16">
          <h2 className="text-3xl md:text-4xl font-bold mb-4">Why Choose GeoCode API?</h2>
          <p className="text-xl text-gray-600">Everything you need for location-based applications</p>
        </div>

        <div className="grid md:grid-cols-3 gap-8">
          <Card>
            <CardHeader>
              <Zap className="h-12 w-12 text-blue-600 mb-4" />
              <CardTitle>Lightning Fast</CardTitle>
              <CardDescription>
                Sub-100ms response times with optimized database queries and caching
              </CardDescription>
            </CardHeader>
          </Card>

          <Card>
            <CardHeader>
              <Database className="h-12 w-12 text-blue-600 mb-4" />
              <CardTitle>Complete Data</CardTitle>
              <CardDescription>
                33,000+ US ZIP codes with coordinates, boundaries, and demographic data
              </CardDescription>
            </CardHeader>
          </Card>

          <Card>
            <CardHeader>
              <Shield className="h-12 w-12 text-blue-600 mb-4" />
              <CardTitle>Secure & Reliable</CardTitle>
              <CardDescription>
                Enterprise-grade security with 99.9% uptime and robust API key management
              </CardDescription>
            </CardHeader>
          </Card>

          <Card>
            <CardHeader>
              <Code className="h-12 w-12 text-blue-600 mb-4" />
              <CardTitle>Developer Friendly</CardTitle>
              <CardDescription>
                RESTful API with comprehensive documentation and interactive examples
              </CardDescription>
            </CardHeader>
          </Card>

          <Card>
            <CardHeader>
              <Globe className="h-12 w-12 text-blue-600 mb-4" />
              <CardTitle>Distance Calculations</CardTitle>
              <CardDescription>
                Built-in distance and proximity calculations between any ZIP codes
              </CardDescription>
            </CardHeader>
          </Card>

          <Card>
            <CardHeader>
              <CheckCircle2 className="h-12 w-12 text-blue-600 mb-4" />
              <CardTitle>Always Up-to-Date</CardTitle>
              <CardDescription>
                Regular updates ensure you always have the latest ZIP code information
              </CardDescription>
            </CardHeader>
          </Card>
        </div>
      </div>

      {/* API Example */}
      <div className="bg-gray-50 py-24">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="text-center mb-16">
            <h2 className="text-3xl md:text-4xl font-bold mb-4">Simple to Use</h2>
            <p className="text-xl text-gray-600">Get started in minutes with our clean, intuitive API</p>
          </div>

          <Card className="max-w-3xl mx-auto">
            <CardHeader>
              <CardTitle>Example Request</CardTitle>
            </CardHeader>
            <CardContent>
              <pre className="bg-gray-900 text-green-400 p-6 rounded-lg overflow-x-auto text-sm">
{`curl -X GET "https://api.example.com/api/v1/geocode/10001" \\
  -H "X-API-Key: your_api_key"

# Response
{
  "success": true,
  "data": {
    "zip_code": "10001",
    "city": "New York",
    "state": "NY",
    "latitude": 40.7506,
    "longitude": -73.9971
  }
}`}
              </pre>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* CTA */}
      <div className="bg-gradient-to-r from-blue-600 to-purple-600 text-white py-24">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 text-center">
          <h2 className="text-3xl md:text-4xl font-bold mb-6">Ready to Get Started?</h2>
          <p className="text-xl mb-8 text-blue-100 max-w-2xl mx-auto">
            Join thousands of developers using GeoCode API for their location-based applications
          </p>
          <Link to="/auth/signup">
            <Button size="lg" >
              Create Free Account
            </Button>
          </Link>
        </div>
      </div>

      {/* Footer */}
      <footer className="bg-gray-900 text-gray-400 py-12">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="text-center">
            <p>&copy; 2025 GeoCode API. All rights reserved.</p>
            <div className="mt-4 space-x-6">
              <a href="/docs" target="_blank" rel="noopener noreferrer" className="hover:text-white">Documentation</a>
              <a href="/api-docs.yaml" target="_blank" rel="noopener noreferrer" className="hover:text-white">API Spec</a>
            </div>
          </div>
        </div>
      </footer>
    </div>
  )
}
