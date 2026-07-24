import { createBrowserRouter, RouterProvider, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard } from './pages/Dashboard'
import { Queue } from './pages/Queue'
import { History } from './pages/History'
import { Directories } from './pages/Directories'
import { Review } from './pages/Review'
import { Logs } from './pages/Logs'
import { Settings } from './pages/Settings'
import { Login } from './pages/Login'
import { SpaceSaved } from './pages/SpaceSaved'

const router = createBrowserRouter([
  {
    path: '/login',
    element: <Login />,
  },
  {
    element: <Layout />,
    children: [
      { path: '/', element: <Dashboard /> },
      { path: '/queue', element: <Queue /> },
      { path: '/history', element: <History /> },
      { path: '/directories', element: <Directories /> },
      { path: '/review', element: <Review /> },
      { path: '/logs', element: <Logs /> },
      { path: '/settings', element: <Settings /> },
      { path: '/savings', element: <SpaceSaved /> },
      { path: '*', element: <Navigate to="/" replace /> },
    ],
  },
])

export default function App() {
  return <RouterProvider router={router} />
}
