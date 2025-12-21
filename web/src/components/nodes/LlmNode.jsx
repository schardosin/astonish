import { Brain } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function LlmNode({ id, data, selected }) {
  return (
    <OverflowNode 
      id={id}
      data={data} 
      selected={selected}
      icon={Brain} 
      nodeType="LLM"
      iconColor="#8b5cf6"
    />
  )
}
