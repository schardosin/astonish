import { MessageSquare } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function OutputNode({ data, selected }) {
  return (
    <OverflowNode 
      data={data} 
      selected={selected}
      icon={MessageSquare} 
      nodeType="Output"
      iconColor="#9f7aea"
    />
  )
}
