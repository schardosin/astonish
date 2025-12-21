import { MessageSquare } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function OutputNode({ id, data, selected }) {
  return (
    <OverflowNode 
      id={id}
      data={data} 
      selected={selected}
      icon={MessageSquare} 
      nodeType="Output"
      iconColor="#9f7aea"
    />
  )
}
