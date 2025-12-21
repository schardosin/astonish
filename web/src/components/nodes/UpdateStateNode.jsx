import { Settings } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function UpdateStateNode({ id, data, selected }) {
  return (
    <OverflowNode 
      id={id}
      data={data} 
      selected={selected}
      icon={Settings} 
      nodeType="State"
      iconColor="#8b5cf6"
    />
  )
}
