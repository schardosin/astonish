import { Settings } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function UpdateStateNode({ data, selected }) {
  return (
    <OverflowNode 
      data={data} 
      selected={selected}
      icon={Settings} 
      nodeType="State"
      iconColor="#8b5cf6"
    />
  )
}
