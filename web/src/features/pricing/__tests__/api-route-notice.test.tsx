import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ApiRouteNotice } from '../components/api-route-notice'

const routes = [
  {
    route: 'Ordinary',
    url: 'https://ordinary.example/v1',
    description: 'Factor 1',
    color: 'blue',
  },
  {
    route: 'Optimized',
    url: 'https://optimized.example/v1',
    description: 'Factor 1.1',
    color: 'green',
  },
]

describe('pricing route notice', () => {
  it('shows each configured address, its rate description and a copy button', () => {
    render(<ApiRouteNotice routes={routes} />)
    expect(screen.getByRole('region', { name: 'API Addresses' })).toBeVisible()
    for (const route of routes) {
      expect(screen.getByText(route.url)).toBeVisible()
      expect(screen.getByText(route.description)).toBeVisible()
    }
    expect(screen.getAllByRole('button', { name: 'Copy URL' })).toHaveLength(2)
  })

  it('hides the dual route notice when fewer than two routes are enabled', () => {
    const { rerender } = render(<ApiRouteNotice routes={[]} />)
    expect(screen.queryByRole('region', { name: 'API Addresses' })).toBeNull()
    rerender(<ApiRouteNotice routes={routes.slice(0, 1)} />)
    expect(screen.queryByRole('region', { name: 'API Addresses' })).toBeNull()
  })
})
